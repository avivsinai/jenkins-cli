from __future__ import annotations

import sys
from functools import wraps
from typing import (
    Any,
    AsyncIterable,
    AsyncGenerator,
    AsyncContextManager,
    Iterator,
    Awaitable,
    Callable,
    Generic,
    TypeVar,
    cast,
    overload,
)
from ._impl import await_, with_portal_run_sync

T = TypeVar("T")
F = TypeVar("F", bound=Callable[..., Any])
AF = TypeVar("AF", bound=Callable[..., Awaitable[Any]])


def autoawait(fn: Callable[..., Awaitable[T]]) -> Callable[..., T]:
    """Decorator for an async function which allows (and requires) it to be called
    from synchronous contexts without ``await``.

    For example, this can be used for magic methods, property setters, and so on.
    """

    @wraps(fn)
    def wrapper(*args: Any, **kw: Any) -> T:
        return await_(fn(*args, **kw))

    return wrapper


# For signature-preserving decorators we can declare the result as
# signature-preserving too, and catch the case where the inner function isn't async
@overload
def decorate_as_sync(decorator: Callable[[F], F]) -> Callable[[AF], AF]: ...


# For non-signature-preserving, all we can do is say the inner function and
# the decorated function are both async. (This could be improved using ParamSpec
# for decorators that are args-preserving but not return-type-preserving.)
@overload
def decorate_as_sync(
    decorator: Callable[..., Any]
) -> Callable[[Callable[..., Awaitable[Any]]], Callable[..., Awaitable[Any]]]: ...


def decorate_as_sync(decorator: Any) -> Any:
    """Wrap the synchronous function decorator *decorator* so that it can
    be used to decorate an async function.

    This can be used, for example, to apply an async-naive decorator such as
    `@functools.lru_cache() <functools.lru_cache>` to an async function::

        @greenback.decorate_as_sync(functools.lru_cache(maxsize=128))
        async def some_fn(...): ...

    Without the wrapping in :func:`decorate_as_sync`, the LRU cache
    would treat the inner function as a synchronous function, and
    would therefore unhelpfully cache the coroutine object that is
    returned when an async function is called without ``await``.

    Internally, the "inner" async function is wrapped in a synchronous
    function that invokes that async function using
    :func:`greenback.await_`. This synchronous function is then
    decorated with the *decorator*. :func:`decorate_as_sync` returns
    an "outer" async function which invokes the internal decorated
    synchronous function using :func:`greenback.with_portal_run_sync`.

    In other words, the following two calls behave identically::

        result = await greenback.decorate_as_sync(decorator)(async_fn)(*args, **kwds)
        result = await greenback.with_portal_run_sync(
            decorator(greenback.autoawait(async_fn)), *args, **kwds,
        )

    """

    def decorate(async_fn: Any) -> Any:
        @decorator  # type: ignore  # "Untyped decorator makes 'inner' untyped"
        @wraps(async_fn)
        def inner(*args: Any, **kwds: Any) -> Any:
            return await_(async_fn(*args, **kwds))

        @wraps(inner)
        async def outer(*args: Any, **kwds: Any) -> Any:
            return await with_portal_run_sync(inner, *args, **kwds)

        return outer

    return decorate


class async_context(Generic[T]):
    """Wraps an async context manager so it is usable in a synchronous
    ``with`` statement."""

    __slots__ = ("_cm", "_aexit")

    def __init__(self, cm: AsyncContextManager[T]):
        self._cm = cm

    if sys.version_info >= (3, 11):

        def __enter__(self) -> T:
            try:
                aenter = type(self._cm).__aenter__
            except AttributeError:
                raise TypeError(
                    f"{type(self._cm).__name__!r} object does not support the "
                    "asynchronous context manager protocol"
                ) from None
            try:
                self._aexit = type(self._cm).__aexit__
            except AttributeError:
                raise TypeError(
                    f"{type(self._cm).__name__!r} object does not support the "
                    "asynchronous context manager protocol (missed __aexit__ method)"
                ) from None
            return await_(aenter(self._cm))  # type: ignore

    else:

        def __enter__(self) -> T:
            try:
                self._aexit = type(self._cm).__aexit__
            except AttributeError:
                raise AttributeError(
                    f"type object {type(self._cm).__name__!r} has no attribute '__aexit__'"
                ) from None
            aenter = type(self._cm).__aenter__
            return await_(aenter(self._cm))  # type: ignore

    def __exit__(self, *exc: Any) -> bool | None:
        return await_(self._aexit(self._cm, *exc))  # type: ignore


class async_iter(Generic[T]):
    """Wraps an async iterator so it is usable in a synchronous
    ``for`` loop, ``yield from`` statement, or other context that expects
    a synchronous iterator."""

    __slots__ = ("_it",)

    def __init__(self, iterable: AsyncIterable[T]):
        try:
            aiter = type(iterable).__aiter__
        except AttributeError:
            raise TypeError(
                "'async_iter' requires an object with __aiter__ method, got "
                + type(iterable).__name__
            ) from None
        self._it = aiter(iterable)  # type: ignore
        try:
            type(self._it).__anext__
        except AttributeError:
            raise TypeError(
                "'async_iter' received an object from __aiter__ that does not "
                "implement __anext__: " + type(self._it).__name__
            ) from None
        if all(hasattr(self._it, meth) for meth in ("asend", "athrow", "aclose")):
            self.__class__ = async_generator

    def __iter__(self) -> Iterator[T]:
        return self

    def __next__(self) -> T:
        try:
            return await_(type(self._it).__anext__(self._it))  # type: ignore
        except StopAsyncIteration as ex:
            raise StopIteration(*ex.args)


class async_generator(async_iter[T]):
    __slots__ = ()

    def send(self, val: Any) -> T:
        try:
            return await_(cast(AsyncGenerator[T, Any], self._it).asend(val))
        except StopAsyncIteration as ex:
            raise StopIteration(*ex.args)

    def throw(self, *exc: Any) -> T:
        try:
            return await_(cast(AsyncGenerator[T, Any], self._it).athrow(*exc))
        except StopAsyncIteration as ex:
            raise StopIteration(*ex.args)

    def close(self) -> None:
        return await_(cast(AsyncGenerator[T, Any], self._it).aclose())
