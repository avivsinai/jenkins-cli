from datetime import datetime
from typing import Any, Callable, TypeVar

from ._constants import HTTP_DATE_FORMAT


class HTTPDate(str):
    @classmethod
    def __get_validators__(cls):
        yield cls.validate

    @classmethod
    def validate(cls, value: str) -> str:
        try:
            datetime.strptime(value, HTTP_DATE_FORMAT)
        except ValueError:
            raise ValueError(f"Invalid HTTP-date format: {value}")
        return value


F = TypeVar("F", bound=Callable[..., Any])
WrappedFn = TypeVar("WrappedFn", bound=Callable[..., Any])
