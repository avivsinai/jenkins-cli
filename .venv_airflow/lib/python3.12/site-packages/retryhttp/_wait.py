import re
from datetime import datetime, timezone
from typing import Optional, Sequence, Tuple, Type, Union

from pydantic import PositiveFloat, PositiveInt
from tenacity import RetryCallState, wait_exponential, wait_random_exponential
from tenacity.wait import wait_base

from ._constants import HTTP_DATE_FORMAT
from ._utils import (
    get_default_http_status_exceptions,
    get_default_network_errors,
    get_default_timeouts,
    is_rate_limited,
    is_server_error,
)


class wait_from_header(wait_base):
    """Wait strategy that derives the wait value from an HTTP header.

    Value may be either an integer representing the number of seconds to wait
    before retrying, or a future datetime in HTTP-date format, indicating when it is
    acceptable to retry the request.

    More info on HTTP-date format: https://httpwg.org/specs/rfc9110.html#http.date

    Args:
        header (str): Header to attempt to derive wait value from.
        wait_max (float): Maximum time to wait, in seconds. Defaults to 120.0s. If
            `None` is given, will wait indefinitely. Use `Non` with caution, as your
            program will hang if the server responds with an excessive wait value.
        fallback (wait_base): Wait strategy to use if `header` is not present,
            or unable to parse to a `float` value, or if value parsed from header
            exceeds `wait_max`. Defaults to `tenacity.wait_exponential`.

    Raises:
        ValueError: If `fallback` is `None`, and any one of the following is true:
            * header is not present;
            * the value cannot be parsed to a `float`;
            * the value exceeds `wait_max`;
            * the value is a date in the past.

    """

    def __init__(
        self,
        header: str,
        wait_max: Union[PositiveFloat, PositiveInt, None] = 120.0,
        fallback: Optional[wait_base] = wait_exponential(),
    ) -> None:
        self.header = header
        self.wait_max = float(wait_max) if wait_max else None
        self.fallback = fallback

    def _get_wait_value(self, retry_state: RetryCallState) -> float:
        """Attempts parse a wait value from header.

        Args:
            retry_state (RetryCallState): The retry call state of the request.

        Returns:
            float: Seconds to wait, as derived from `self.header`.

        Raises:
            ValueError: If unable to parse a float from `self.header`.

        """
        if retry_state.outcome:
            exc = retry_state.outcome.exception()
            if exc is None:
                return 0
            if isinstance(exc, get_default_http_status_exceptions()) and hasattr(
                exc, "response"
            ):
                value = exc.response.headers.get(self.header)
                if value is None:
                    raise ValueError(f"Header not present: {self.header}")
                if re.match(r"^\d+$", value):
                    return float(value)
                else:
                    retry_after = datetime.strptime(value, HTTP_DATE_FORMAT)
                    retry_after = retry_after.replace(tzinfo=timezone.utc)
                    now = datetime.now(timezone.utc)
                    if retry_after < now:
                        raise ValueError(
                            f'Date provided in header "{self.header}" '
                            f"is in the past: {value}"
                        )
                    return float((retry_after - now).seconds)
        raise ValueError(f'Unable to parse wait time from header: "{self.header}"')

    def __call__(self, retry_state: RetryCallState) -> float:
        if self.fallback:
            try:
                value = self._get_wait_value(retry_state=retry_state)
                if self.wait_max and value > self.wait_max:
                    return self.fallback(retry_state=retry_state)
                return value
            except ValueError:
                return self.fallback(retry_state=retry_state)
        else:
            value = self._get_wait_value(retry_state=retry_state)
            if self.wait_max and value > self.wait_max:
                raise ValueError(
                    f'Wait value parsed from header "{self.header}" ({value}) '
                    f"is greater than `wait_max` ({self.wait_max})"
                )
            return value


class wait_retry_after(wait_from_header):
    """Wait strategy to use when the server responds with a `Retry-After` header.

    The header value may provide a date for when you may retry the request, or an
    integer, indicating the number of seconds to wait before retrying.

    Args:
        wait_max (float): Maximum time to wait, in seconds. Defaults to 120.0s. If
            `None` is given, will wait indefinitely. Use `Non` with caution, as your
            program will hang if the server responds with an excessive wait value.
        fallback (wait_base): Wait strategy to use if `header` is not present,
            or unable to parse to a `float` value, or if value parsed from header
            exceeds `wait_max`. Defaults to `tenacity.wait_exponential()`.

    Raises:
        ValueError: If `fallback` is `None`, and any one of the following is true:
            * `Retry-After` header is not present;
            * the value cannot be parsed to a `float`;
            * the value exceeds `wait_max`;
            * the value is a date in the past.

    """

    def __init__(
        self,
        wait_max: Union[PositiveFloat, PositiveInt, None] = 120.0,
        fallback: Optional[wait_base] = wait_exponential(),
    ) -> None:
        super().__init__(header="Retry-After", wait_max=wait_max, fallback=fallback)


# Aliased for backwards compatibility and convenience
wait_rate_limited = wait_retry_after


class wait_context_aware(wait_base):
    """Uses a different wait strategy based on the type of HTTP error.

    Args:
        wait_server_errors: Wait strategy to use with server errors.
        wait_network_errors: Wait strategy to use with network errors.
        wait_timeouts: Wait strategy to use with timeouts.
        wait_rate_limited: Wait strategy to use when rate limited.
        server_error_codes: One or more 5xx HTTP status codes that will trigger
            `wait_server_errors`.
        network_errors: One or more exceptions that will trigger `wait_network_errors`.
            If omitted, defaults to:

            - `httpx.ConnectError`
            - `httpx.ReadError`
            - `httpx.WriteError`
            - `requests.ConnectionError`
            - `requests.exceptions.ChunkedEncodingError`
        timeouts: One or more exceptions that will trigger `wait_timeouts`. If omitted,
            defaults to:

            - `httpx.ConnectTimeout`
            - `httpx.ReadTimeout`
            - `httpx.WriteTimeout`
            - `requests.Timeout`

    """

    def __init__(
        self,
        wait_server_errors: wait_base = wait_retry_after(
            fallback=wait_random_exponential(),
        ),
        wait_network_errors: wait_base = wait_exponential(),
        wait_timeouts: wait_base = wait_random_exponential(),
        wait_rate_limited: wait_base = wait_retry_after(
            fallback=wait_exponential(),
        ),
        server_error_codes: Union[Sequence[int], int] = (500, 502, 503, 504),
        network_errors: Union[
            Type[BaseException], Tuple[Type[BaseException], ...], None
        ] = None,
        timeouts: Union[
            Type[BaseException], Tuple[Type[BaseException], ...], None
        ] = None,
    ) -> None:
        if network_errors is None:
            network_errors = get_default_network_errors()
        if timeouts is None:
            timeouts = get_default_timeouts()
        self.wait_server_errors = wait_server_errors
        self.wait_network_errors = wait_network_errors
        self.wait_timeouts = wait_timeouts
        self.wait_rate_limited = wait_rate_limited
        self.server_error_codes = server_error_codes
        self.network_errors = network_errors
        self.timeouts = timeouts

    def __call__(self, retry_state: RetryCallState) -> float:
        if retry_state.outcome:
            exc = retry_state.outcome.exception()
            if is_server_error(exc=exc, status_codes=self.server_error_codes):
                return self.wait_server_errors(retry_state)
            if isinstance(exc, self.network_errors):
                return self.wait_network_errors(retry_state)
            if isinstance(exc, self.timeouts):
                return self.wait_timeouts(retry_state)
            if is_rate_limited(exc):
                return self.wait_rate_limited(retry_state)
        return 0
