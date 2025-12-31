from typing import Any, Callable, Optional, Sequence, Tuple, Type, Union, overload

from tenacity import (
    RetryCallState,
    retry_any,
    retry_base,
    retry_if_exception_type,
    stop_after_attempt,
    wait_exponential,
    wait_random_exponential,
)
from tenacity import (
    retry as tenacity_retry,
)
from tenacity.wait import wait_base

from ._types import F
from ._utils import (
    get_default_network_errors,
    get_default_timeouts,
    is_rate_limited,
    is_server_error,
)
from ._wait import wait_context_aware, wait_retry_after


@overload
def retry(func: F) -> F: ...


@overload
def retry(
    func: None = None,
    *,
    max_attempt_number: int = 3,
    retry_server_errors: bool = True,
    retry_network_errors: bool = True,
    retry_timeouts: bool = True,
    retry_rate_limited: bool = True,
    wait_server_errors: wait_base = wait_random_exponential(),
    wait_network_errors: wait_base = wait_exponential(),
    wait_timeouts: wait_base = wait_random_exponential(),
    wait_rate_limited: wait_base = wait_retry_after(),
    server_error_codes: Union[Sequence[int], int] = (500, 502, 503, 504),
    network_errors: Union[
        Type[BaseException], Tuple[Type[BaseException], ...], None
    ] = None,
    timeouts: Union[Type[BaseException], Tuple[Type[BaseException], ...], None] = None,
    **kwargs: Any,
) -> Callable[[F], F]: ...


def retry(
    func: Optional[F] = None,
    *,
    max_attempt_number: int = 3,
    retry_server_errors: bool = True,
    retry_network_errors: bool = True,
    retry_timeouts: bool = True,
    retry_rate_limited: bool = True,
    wait_server_errors: wait_base = wait_random_exponential(),
    wait_network_errors: wait_base = wait_exponential(),
    wait_timeouts: wait_base = wait_random_exponential(),
    wait_rate_limited: wait_base = wait_retry_after(),
    server_error_codes: Union[Sequence[int], int] = (500, 502, 503, 504),
    network_errors: Union[
        Type[BaseException], Tuple[Type[BaseException], ...], None
    ] = None,
    timeouts: Union[Type[BaseException], Tuple[Type[BaseException], ...], None] = None,
    **kwargs: Any,
) -> Union[F, Callable[[F], F]]:
    """Retry potentially transient HTTP errors with sensible default behavior.

    By default, retries the following errors, for a total of 3 attempts, with
    exponential backoff (except when rate limited, which defaults to the
    `Retry-After` header, if present):

    - HTTP status errors:
        - `429 Too Many Requests` (rate limited)
        - `500 Internal Server Error`
        - `502 Bad Gateway`
        - `503 Service Unavailable`
        - `504 Gateway Timeout`
    - Network errors:
        - `httpx.ConnectError`
        - `httpx.ReadError`
        - `httpx.WriteError`
        - `requests.ConnectionError`
        - `requests.exceptions.ChunkedEncodingError`
    - Timeouts:
        - `httpx.TimeoutException`
        - `requests.Timeout`

    Args:
        max_attempt_number: Total times to attempt a request. Includes the first attempt
            and any additional retries.
        retry_server_errors: Whether to retry 5xx server errors.
        retry_network_errors: Whether to retry network errors.
        retry_timeouts: Whether to retry timeouts.
        retry_rate_limited: Whether to retry when `Retry-After` header received.
        wait_server_errors: Wait strategy to use for server errors.
        wait_network_errors: Wait strategy to use for network errors.
        wait_timeouts: Wait strategy to use for timeouts.
        wait_rate_limited: Wait strategy to use when `Retry-After` header received.
        server_error_codes: One or more 5xx error codes that will trigger `wait_server_errors`
            if `retry_server_errors` is `True`. Defaults to 500, 502, 503, and 504.
        network_errors: One or more exceptions that will trigger `wait_network_errors` if
            `retry_network_errors` is `True`. Defaults to:

            - `httpx.ConnectError`
            - `httpx.ReadError`
            - `httpx.WriteError`
            - `requests.ConnectError`
            - `requests.exceptions.ChunkedEncodingError`
        timeouts: One or more exceptions that will trigger `wait_timeouts` if
            `retry_timeouts` is `True`. Defaults to:

            - `httpx.TimeoutException`
            - `requests.Timeout`

    Returns:
        Decorated function.

    Raises:
        RuntimeError: If `retry_server_errors`, `retry_network_errors`, `retry_timeouts`,
            and `retry_rate_limited` are all `False`.

    """
    if network_errors is None:
        network_errors = get_default_network_errors()
    if timeouts is None:
        timeouts = get_default_timeouts()

    retry_strategies: list[retry_base] = []
    if retry_server_errors:
        retry_strategies.append(
            retry_if_server_error(server_error_codes=server_error_codes)
        )
    if retry_network_errors:
        retry_strategies.append(retry_if_network_error(errors=network_errors))
    if retry_timeouts:
        retry_strategies.append(retry_if_timeout(timeouts=timeouts))
    if retry_rate_limited:
        retry_strategies.append(retry_if_rate_limited())
    if not retry_strategies:
        raise RuntimeError("No retry strategies enabled.")

    retry = kwargs.pop("retry", None) or retry_any(*retry_strategies)

    # We don't need to conditionally build our wait strategy since each strategy
    # will only apply if the corresponding retry strategy is in use.
    wait = kwargs.pop("wait", None) or wait_context_aware(
        wait_server_errors=wait_server_errors,
        wait_network_errors=wait_network_errors,
        wait_timeouts=wait_timeouts,
        wait_rate_limited=wait_rate_limited,
        server_error_codes=server_error_codes,
        network_errors=network_errors,
        timeouts=timeouts,
    )

    stop = kwargs.pop("stop", None) or stop_after_attempt(max_attempt_number)

    def decorator(func: F) -> F:
        return tenacity_retry(retry=retry, wait=wait, stop=stop, **kwargs)(func)

    if func:
        return decorator(func)
    return decorator


class retry_if_network_error(retry_if_exception_type):
    """Retry network errors.

    Args:
        errors: One or more exceptions to consider a network error. If omitted,
            defaults to:

            - `httpx.ConnectError`
            - `httpx.ReadError`
            - `httpx.WriteError`
            - `requests.ConnectionError`
            - `requests.exceptions.ChunkedEncodingError`

    """

    def __init__(
        self,
        errors: Union[
            Type[BaseException], Tuple[Type[BaseException], ...], None
        ] = None,
    ) -> None:
        if errors is None:
            errors = get_default_network_errors()
        self.errors = errors
        super().__init__(exception_types=self.errors)


class retry_if_rate_limited(retry_base):
    """Retry if server responds with a `Retry-After` header."""

    def __call__(self, retry_state: RetryCallState) -> bool:
        if retry_state.outcome and retry_state.outcome.failed:
            return is_rate_limited(retry_state.outcome.exception())
        return False


class retry_if_server_error(retry_base):
    """Retry certain server errors (5xx).

    Args:
        server_error_codes: One or more 5xx errors to retry.

    """

    def __init__(
        self,
        server_error_codes: Union[Sequence[int], int] = (500, 502, 503, 504),
    ) -> None:
        self.server_error_codes = server_error_codes

    def __call__(self, retry_state: RetryCallState) -> bool:
        if retry_state.outcome and retry_state.outcome.failed:
            exc = retry_state.outcome.exception()
            return is_server_error(exc, self.server_error_codes)
        return False


class retry_if_timeout(retry_if_exception_type):
    """Retry timeouts.

    Args:
        timeouts: One or more exceptions to consider a timeout. If omitted,
            defaults to:

            - `httpx.ConnectTimeout`
            - `httpx.ReadTimeout`
            - `httpx.WriteTimeout`
            - `requests.Timeout`
    """

    def __init__(
        self,
        timeouts: Union[
            Type[BaseException], Tuple[Type[BaseException], ...], None
        ] = None,
    ) -> None:
        if timeouts is None:
            timeouts = get_default_timeouts()
        self.timeouts = timeouts
        super().__init__(exception_types=self.timeouts)
