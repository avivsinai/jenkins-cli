from datetime import datetime, timedelta, timezone
from typing import Optional, Sequence, Tuple, Type, Union

from ._constants import HTTP_DATE_FORMAT
from ._types import HTTPDate

_HTTPX_INSTALLED = False
_REQUESTS_INSTALLED = False

try:
    import httpx

    _HTTPX_INSTALLED = True
except ImportError:
    pass

try:
    import requests

    _REQUESTS_INSTALLED = True
except ImportError:
    pass


def get_default_network_errors() -> Tuple[Type[BaseException], ...]:
    """Get all network errors to use by default.

    Args:
        N/A

    Returns:
        Tuple of network error exceptions.

    Raises:
        N/A

    """
    exceptions: list[type[BaseException]] = []
    if _HTTPX_INSTALLED:
        exceptions.extend(
            [
                httpx.ConnectError,
                httpx.ReadError,
                httpx.WriteError,
            ]
        )
    if _REQUESTS_INSTALLED:
        exceptions.extend(
            [
                requests.ConnectionError,
                requests.exceptions.ChunkedEncodingError,
            ]
        )
    return tuple(exceptions)


def get_default_timeouts() -> Tuple[Type[BaseException], ...]:
    """Get all timeout exceptions to use by default.

    Returns:
        tuple: Timeout exceptions.

    """
    exceptions: list[Type[BaseException]] = []
    if _HTTPX_INSTALLED:
        exceptions.append(httpx.TimeoutException)
    if _REQUESTS_INSTALLED:
        exceptions.append(requests.Timeout)
    return tuple(exceptions)


def get_default_http_status_exceptions() -> Tuple[Type[BaseException], ...]:
    """Get default HTTP status 4xx or 5xx exceptions.

    Returns:
        tuple: HTTP status exceptions.

    """
    exceptions: list[Type[BaseException]] = []
    if _HTTPX_INSTALLED:
        exceptions.append(httpx.HTTPStatusError)
    if _REQUESTS_INSTALLED:
        exceptions.append(requests.HTTPError)
    return tuple(exceptions)


def is_rate_limited(exc: Optional[BaseException]) -> bool:
    """Whether a given exception indicates the user has been rate limited.

    Args:
        exc: Exception to consider.

    Returns:
        bool: Whether exc indicates rate limiting.

    """
    if exc is None:
        return False
    exceptions = get_default_http_status_exceptions()
    if isinstance(exc, exceptions) and hasattr(exc, "response"):
        return exc.response.status_code == 429
    return False


def is_server_error(
    exc: Optional[BaseException],
    status_codes: Union[Sequence[int], int] = tuple(range(500, 600)),
) -> bool:
    """Whether a given exception indicates a 5xx server error.

    Args:
        exc: Exception to consider.
        status_codes: One or more 5xx status codes to consider. Defaults
            to all (500-599).

    Returns:
        bool: whether exc indicates an error included in status_codes.

    """
    if exc is None:
        return False
    if isinstance(status_codes, int):
        status_codes = [status_codes]
    exceptions = get_default_http_status_exceptions()
    if isinstance(exc, exceptions) and hasattr(exc, "response"):
        return exc.response.status_code in status_codes
    return False


def get_http_date(delta_seconds: int = 0) -> HTTPDate:
    """Builds a valid HTTP-date timestamp string according to RFC 7231.

    By default, returns an HTTP-date string for the current timestamp.
    May be offset by a positive or negative integer, in seconds.

    Args:
        delta_seconds (int): Number of seconds to offset the timestamp
            by. If a negative integer is passed, result will be in the
            past.

    Returns:
        HTTPDate: A valid HTTP-date string.

    """
    date = datetime.now(timezone.utc)
    if delta_seconds:
        date = date + timedelta(seconds=delta_seconds)
    return HTTPDate(date.strftime(HTTP_DATE_FORMAT))
