import importlib
import inspect
import pytest
from functools import partial

# Tests declared with a "library" parameter will run twice, once under asyncio
# with the parameter taking on the string value "asyncio", and once under Trio
# with the parameter value "trio". Tests declared without such a parameter
# run only under Trio using the autojump clock.


def pytest_generate_tests(metafunc):
    if "library" in metafunc.fixturenames:
        avail = []
        for lib in ("asyncio", "trio"):
            try:
                importlib.import_module(lib)
            except ImportError:  # pragma: no cover
                pass
            else:
                avail.append(lib)
        metafunc.parametrize("library", avail)


@pytest.hookimpl(hookwrapper=True)
def pytest_runtest_call(item):
    if inspect.iscoroutinefunction(item.obj):
        orig_func = item.obj

        def run_async_test(**kw):
            if "library" in kw:
                import anyio

                anyio.run(partial(orig_func, **kw), backend=kw["library"])
            else:
                import trio
                import trio.testing

                trio.run(
                    partial(orig_func, **kw),
                    clock=trio.testing.MockClock(autojump_threshold=0),
                )

        item.obj = run_async_test

    yield
