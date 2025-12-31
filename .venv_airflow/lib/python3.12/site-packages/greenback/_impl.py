from __future__ import annotations

import collections
import greenlet  # type: ignore
import outcome
import sniffio
import sys
import types
import weakref
from functools import partial
from typing import (
    Any,
    Awaitable,
    Callable,
    Coroutine,
    Generator,
    MutableMapping,
    TypeVar,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    import trio
    import asyncio

try:
    import contextvars
except ImportError:  # pragma: no cover
    # 'no cover' rationale: Trio pulls in the contextvars backport,
    # and it's not worth adding more CI runs in environments that
    # specifically exclude Trio.
    if not TYPE_CHECKING:
        contextvars = None

T = TypeVar("T")

# Dictionary whose keys are tasks (trio.lowlevel.Task or asyncio.Task)
# that have a "greenback portal" installed, via any of the *_portal()
# functions, and whose values are the corresponding greenlets that implement
# the portal. When running, these tasks can send event loop traps to their
# portal greenlet in order to yield them to the event loop.
#
# Immediately after a task has been portalized, but before its first tick
# runs, its value in this mapping will be None, because we don't yet know
# which greenlet is running its greenback_shim coroutine. That's fine because
# we can't reach an await_ in the new task until its first tick runs.
task_portals: MutableMapping[object, greenlet.greenlet | None] = (
    weakref.WeakKeyDictionary()
)

# The offset of asyncio.Task._coro in the Task object memory layout, if
# asyncio.Task is implemented in C (which it generally is on CPython 3.6+).
# This is determined dynamically when it is first needed.
aio_task_coro_c_offset: int | None = None

# If True, we need to configure greenlet to preserve our
# contextvars context when we switch greenlets. (Older versions
# of greenlet are context-naive and do what we want by default.
# greenlet v0.4.17 tried to be context-aware but isn't configurable
# to get the behavior we want; we forbid it in our setup.py dependencies.)
# See https://github.com/python-greenlet/greenlet/issues/196 for details.
greenlet_needs_context_fixup: bool = contextvars is not None and getattr(
    greenlet, "GREENLET_USE_CONTEXT_VARS", False
)


def trampoline(
    portal_greenlet: greenlet.greenlet,
    orig_coro: Coroutine[Any, Any, Any],
    next_send: outcome.Outcome[Any],
) -> Any:
    """Smooths over the interface differences between an event loop trap
    encountered during an await_() and one encountered during a native await.
    Run this function as the target of a greenlet; it will send (event loop
    trap, greenlet to resume) tuples to the `portal_greenlet` and resume the
    `orig_coro` with the outcomes sent back in reply.
    """
    this_greenlet = greenlet.getcurrent()
    while True:
        # StopIteration or other exceptions will escape from this function
        next_yield: Any = next_send.send(orig_coro)  # type: ignore
        next_send = portal_greenlet.switch((next_yield, this_greenlet))


@types.coroutine
def async_yield_ready() -> Generator[Any, Any, Any]:
    return (yield "ready")


async def greenback_shim(task: object, orig_coro: Coroutine[Any, Any, Any]) -> Any:
    """When a task has called ensure_portal(), its coroutine object is a coroutine
    for this function. This function then invokes each step of the task's original
    coroutine in a context that allows suspending via greenlet.
    """

    # This wrapper serves two purposes:
    #
    # - It ensures that the top-level task coroutine is actually a coroutine,
    #   not a generator. Some Trio introspection tools care about the
    #   difference, as does anyio.
    #
    # - It yields a sentinel value as its first action, so that it is then
    #   ready to be resumed with something meaningful. This resolves an
    #   impedance mismatch: the first send into a new coroutine object must
    #   be None, but the next send into an already-running task might not
    #   want to be None.
    #
    # Portalizing an already-running task (bestow_portal(), ensure_portal())
    # uses this wrapper. Creating a portal around a new function
    # (with_portal_run(), with_portal_run_tree()) uses the inner
    # _greenback_shim directly.

    next_send = await outcome.acapture(async_yield_ready)
    task_portals[task] = greenlet.getcurrent()
    try:
        return await _greenback_shim(orig_coro, next_send)  # type: ignore
    finally:
        del task_portals[task]


@types.coroutine
def _greenback_shim(
    orig_coro: Coroutine[Any, Any, Any], next_send: outcome.Outcome[Any]
) -> Generator[Any, Any, Any]:
    # In theory this could be written as a simpler function that uses
    # _greenback_shim_sync():
    #
    #     next_yield = "ready"
    #     while True:
    #         try:
    #             target = partial(orig_coro.send, (yield next_yield))
    #         except BaseException as ex:
    #             target = partial(orig_coro.throw, ex)
    #         try:
    #             next_yield = yield from _greenback_shim_sync(target)
    #         except StopIteration as ex:
    #             return ex.value
    #
    # In practice, this doesn't work that well: _greenback_shim_sync()
    # has a hard time raising StopIteration, because it's a generator,
    # and unrolling it into a non-generator iterable makes it slower.
    # So we'll accept a bit of code duplication.

    # The greenlet running this _greenback_shim function, which implements
    # the portal. We receive tuples (event loop trap, greenlet to resume
    # with result of yielding this trap) and pass them through our parent
    # event loop / coroutine runner.
    portal_greenlet = greenlet.getcurrent()

    # The greenlet running orig_coro, which uses the portal. We wrap it
    # in a trampoline() helper function, defined above, in order to simplify
    # the control flow. This way we only have to create one greenlet per
    # portal, instead of one per event loop trap that uses the portal; this
    # improves efficiency.
    child_greenlet = greenlet.greenlet(partial(trampoline, portal_greenlet, orig_coro))

    # The greenlet to which we will send the next thing our event loop
    # sends us. This is initially the child_greenlet, but if the child_greenlet
    # starts its own nested child greenlets and those want to use await_(),
    # they'll be able to. Distinguishing resume_greenlet from child_greenlet
    # is important for interoperability with other greenback-like systems,
    # such as sqlalchemy's async ORM support.
    resume_greenlet = child_greenlet

    # The contextvars.Context that we have most recently seen as active
    # for this task and propagated to child_greenlet
    curr_ctx: contextvars.Context | None = None

    while True:
        if (
            greenlet_needs_context_fixup
            and portal_greenlet.gr_context is not curr_ctx
            and child_greenlet.gr_context is curr_ctx
        ):
            # Make sure the child greenlet's contextvars context
            # is the same as our own, even if our own context
            # changes (such as via trio.Task.context assignment),
            # unless the child greenlet appears to have changed
            # its context privately through a call to Context.run().
            #
            # We only fix up our immediate child. If it spawns its own
            # grandchild greenlet(s), it's responsible for propagating
            # contextvars to those.
            #
            # Note 'portal_greenlet.gr_context' here is just a
            # portable way of getting the current contextvars
            # context, which is not exposed by the contextvars
            # module directly (copy_context() returns a copy, not
            # a new reference to the original).  Upon initial
            # creation of child_greenlet, curr_ctx and
            # child_greenlet.gr_context will both be None, so this
            # condition works for that case too.
            child_greenlet.gr_context = curr_ctx = portal_greenlet.gr_context

        try:
            # Forward the event loop's resumption message into our child.
            # It will proceed until the next event loop trap and send us
            # that trap + the identity of the greenlet that reached the trap;
            # the latter is so we can resume the correct greenlet next time
            # in case of nested child greenlets.
            next_yield, resume_greenlet = resume_greenlet.switch(next_send)
        except StopIteration as ex:
            # The underlying coroutine completed, so we forward its return value.
            return ex.value
        # If the underlying coroutine raises any other exception, it will
        # propagate out of _greenback_shim, which is what we want.

        try:
            # Normally we send to orig_coro whatever the event loop sent us
            next_send = outcome.Value((yield next_yield))
        except BaseException as ex:
            # If the event loop resumed us with an error, we forward that error
            next_send = outcome.Error(ex)


@types.coroutine
def _greenback_shim_sync(target: Callable[[], Any]) -> Generator[Any, Any, Any]:
    """Run target(), forwarding the event loop traps and responses necessary
    to implement any await_() calls that it makes.

    This gives a nice speed boost over using greenback_shim() plus a
    sync-to-async wrapper (6 microseconds to create a sync portal versus
    16 for async, on the author's machine), though that's probably only
    relevant when you're scoping the portal to a very small range.
    We ship it anyway because it's easier to understand than
    the async-compatible _greenback_shim(), and helps with understanding
    the latter.
    """

    portal_greenlet = greenlet.getcurrent()
    curr_ctx = None

    # The greenlet in which we run target().
    child_greenlet = greenlet.greenlet(target)

    # The greenlet currently suspended in await_()
    resume_greenlet = child_greenlet

    # The next thing we plan to yield to the event loop.
    next_yield: Any

    # The next thing we plan to send via greenlet.switch(). This is an
    # outcome representing the value or error that the event loop resumed
    # us with. Initially None for the very first zero-argument switch().
    next_send: outcome.Outcome[Any] | None = None

    while True:
        if (
            greenlet_needs_context_fixup
            and portal_greenlet.gr_context is not curr_ctx
            and child_greenlet.gr_context is curr_ctx
        ):
            # Make sure the child greenlet's contextvars context
            # is the same as our own, even if our own context
            # changes (such as via trio.Task.context assignment),
            # unless the child greenlet appears to have changed
            # its context privately through a call to Context.run().
            #
            # We only fix up our immediate child. If it spawns its own
            # grandchild greenlet(s), it's responsible for propagating
            # contextvars to those.
            #
            # Note 'portal_greenlet.gr_context' here is just a
            # portable way of getting the current contextvars
            # context, which is not exposed by the contextvars
            # module directly (copy_context() returns a copy, not
            # a new reference to the original).  Upon initial
            # creation of child_greenlet, curr_ctx and
            # child_greenlet.gr_context will both be None, so this
            # condition works for that case too.
            child_greenlet.gr_context = curr_ctx = portal_greenlet.gr_context

        # Forward the event loop's resumption message into our child.
        # It will proceed until the next event loop trap and send us
        # that trap + the identity of the greenlet that reached the trap;
        # the latter is so we can resume the correct greenlet next time
        # in case of nested child greenlets.
        if next_send is None:
            request = resume_greenlet.switch()
        else:
            request = resume_greenlet.switch(next_send)

        if child_greenlet.dead:
            # target() returned, so `request` is its return value, rather than
            # a (next_yield, resume_greenlet) tuple. (If target() exits with an
            # exception, that exception will propagate out of switch() and thus
            # out of the loop, which is what we want.)
            return request

        next_yield, resume_greenlet = request

        try:
            # Normally we send target() whatever the event loop sent us
            next_send = outcome.Value((yield next_yield))
        except BaseException as ex:
            # If the event loop resumed us with an error, we forward that error
            next_send = outcome.Error(ex)


def current_task() -> trio.lowlevel.Task | asyncio.Task[Any]:
    library = sniffio.current_async_library()
    if library == "trio":
        try:
            from trio.lowlevel import current_task
        except ImportError:  # pragma: no cover
            if not TYPE_CHECKING:
                from trio.hazmat import current_task

        return current_task()
    elif library == "asyncio":
        import asyncio

        if sys.version_info >= (3, 7):
            task = asyncio.current_task()
        else:  # pragma: no cover
            task = asyncio.Task.current_task()
        if task is None:
            raise RuntimeError("No asyncio task is running")
        return task
    else:
        raise RuntimeError(f"greenback does not support {library}")


def _aligned_ptr_offset_in_object(obj: object, referent: object) -> int | None:
    """Return the byte offset in the C representation of *obj* (an
    arbitrary Python object) at which is found a naturally-aligned
    pointer that points to *referent*.  If *search_for*
    can't be found, return None.
    """
    import ctypes

    size = obj.__sizeof__()
    arraytype = ctypes.c_size_t * (size // ctypes.sizeof(ctypes.c_size_t))
    for idx, value in enumerate(arraytype.from_address(id(obj))):
        if value == id(referent):
            return idx * ctypes.sizeof(ctypes.c_size_t)
    return None


def set_aio_task_coro(
    task: asyncio.Task[Any], new_coro: Coroutine[Any, Any, Any]
) -> None:
    try:
        task._coro = new_coro  # type: ignore
        return
    except AttributeError as ex:
        if sys.implementation.name != "cpython":  # pragma: no cover
            raise
        if "is not writable" not in str(ex):  # pragma: no cover
            raise

    # If using the C accelerator on CPython, the setter isn't
    # even present and we need to use ctypes to change the
    # coroutine field.

    global aio_task_coro_c_offset
    import ctypes

    old_coro = task.get_coro()

    if aio_task_coro_c_offset is None:
        # Deduce the offset by scanning the task object representation
        # for id(task._coro)
        aio_task_coro_c_offset = _aligned_ptr_offset_in_object(task, old_coro)
        if aio_task_coro_c_offset is None:  # pragma: no cover
            raise RuntimeError("Couldn't determine C offset of asyncio.Task._coro")

    # (Explanation copied from trio._core._multierror, applies equally well here.)
    # How to handle refcounting? I don't want to use ctypes.py_object because
    # I don't understand or trust it, and I don't want to use
    # ctypes.pythonapi.Py_{Inc,Dec}Ref because we might clash with user code
    # that also tries to use them but with different types. So private _ctypes
    # APIs it is!
    import _ctypes

    coro_field = ctypes.c_size_t.from_address(id(task) + aio_task_coro_c_offset)
    assert coro_field.value == id(old_coro)
    _ctypes.Py_INCREF(new_coro)  # type: ignore
    coro_field.value = id(new_coro)
    _ctypes.Py_DECREF(old_coro)  # type: ignore


def bestow_portal(task: trio.lowlevel.Task | asyncio.Task[Any]) -> None:
    """Ensure that the given async *task* is able to use :func:`greenback.await_`.

    This works like calling :func:`ensure_portal` from within *task*,
    with one exception: if you pass the currently running task, then
    the portal will not become usable until after the task yields
    control to the event loop.
    """

    if task in task_portals:
        # This task already has a greenback shim; nothing to do.
        return

    # Create the shim coroutine
    if type(task).__module__.startswith("trio."):
        try:
            from trio.lowlevel import Task
        except ImportError:  # pragma: no cover
            if not TYPE_CHECKING:
                from trio.hazmat import Task

        assert isinstance(task, Task)
        shim_coro = greenback_shim(task, task.coro)
        commit: Callable[[], None] = partial(setattr, task, "coro", shim_coro)
    else:
        import asyncio

        assert isinstance(task, asyncio.Task)
        shim_coro = greenback_shim(task, task.get_coro())
        commit = partial(set_aio_task_coro, task, shim_coro)

    # Step it once so it's ready to get resumed by the event loop
    first_yield = shim_coro.send(None)
    assert first_yield == "ready"

    # Update so the event loop will resume shim_coro rather than the
    # original task coroutine
    commit()

    # Note that this task has been portalized so we don't try to do it again.
    # Its parent greenlet (the value in this mapping) will be set on its
    # next tick, enabling greenback.await_() for this task.
    task_portals[task] = None


async def ensure_portal() -> None:
    """Ensure that the current async task is able to use :func:`greenback.await_`.

    If the current task has called :func:`ensure_portal` previously, calling
    it again is a no-op. Otherwise, :func:`ensure_portal` interposes a
    "coroutine shim" provided by `greenback` in between the event
    loop and the coroutine being used to run the task. For example,
    when running under Trio, `trio.lowlevel.Task.coro` is replaced with
    a wrapper around the coroutine it previously referred to. (The
    same thing happens under asyncio, but asyncio doesn't expose the
    coroutine field publicly, so some additional trickery is required
    in that case.)

    After installation of the coroutine shim, each task step passes
    through `greenback` on its way into and out of your code. At
    some performance cost, this effectively provides a **portal** that
    allows later calls to :func:`greenback.await_` in the same task to
    access an async environment, even if the function that calls
    :func:`await_` is a synchronous function.

    This function is a cancellation point and a schedule point (a checkpoint,
    in Trio terms) even if the calling task already had a portal set up.
    """

    this_task = current_task()
    if this_task not in task_portals:
        bestow_portal(this_task)

    # Execute a checkpoint so that we're now running inside the shim coroutine.
    # This is necessary in case the caller immediately invokes greenback.await_()
    # without any further checkpoints.
    library = sniffio.current_async_library()
    await sys.modules[library].sleep(0)


def has_portal(task: trio.lowlevel.Task | asyncio.Task[Any] | None = None) -> bool:
    """Return true if the given *task* is currently able to use
    :func:`greenback.await_`, false otherwise. If no *task* is
    specified, query the currently executing task.
    """
    if task is not None and task_portals.get(task) is not None:
        return True

    try:
        this_task = current_task()
    except (RuntimeError, sniffio.AsyncLibraryNotFoundError):
        this_task = None

    if task is None:
        task = this_task
        if task is None:
            return False

    if task is this_task:
        # For the currently running task, an entry in task_portals
        # with value None means a portal has been bestowed but won't
        # be active until the next checkpoint. The answer to "can I run
        # await_()" in that case is therefore "no".
        return task_portals.get(task) is not None
    else:
        # For a task that's not currently running, even an inactive portal
        # will be activated before the task can do anything, so we return
        # True here.
        return task in task_portals


async def with_portal_run(
    async_fn: Callable[..., Awaitable[T]], *args: Any, **kwds: Any
) -> T:
    """Execute ``await async_fn(*args, **kwds)`` in a context that is able
    to use :func:`greenback.await_`.

    If the current task already has a greenback portal set up via a
    call to one of the other ``greenback.*_portal()`` functions, then
    :func:`with_portal_run` simply calls *async_fn*.  If *async_fn*
    uses :func:`greenback.await_`, the existing portal will take care
    of it.

    Otherwise (if there is no portal already available to the current task),
    :func:`with_portal_run` creates a new portal which lasts only for the
    duration of the call to *async_fn*. If *async_fn* then calls
    :func:`ensure_portal`, an additional portal will **not** be created:
    the task will still have just the portal installed by
    :func:`with_portal_run`, which will be removed when *async_fn* returns.

    This function does *not* add any cancellation point or schedule point
    beyond those that already exist inside *async_fn*.
    """

    this_task = current_task()
    if this_task in task_portals:
        return await async_fn(*args, **kwds)
    task_portals[this_task] = greenlet.getcurrent()
    try:
        coro = async_fn(*args, **kwds)
        if not isinstance(coro, collections.abc.Coroutine):
            coro = adapt_awaitable(coro)
        res: T = await _greenback_shim(coro, outcome.Value(None))
        return res
    finally:
        del task_portals[this_task]


async def with_portal_run_sync(sync_fn: Callable[..., T], *args: Any, **kwds: Any) -> T:
    """Execute ``sync_fn(*args, **kwds)`` in a context that is able
    to use :func:`greenback.await_`.

    If the current task already has a greenback portal set up via a
    call to one of the other ``greenback.*_portal()`` functions, then
    :func:`with_portal_run` simply calls *sync_fn*.  If *sync_fn*
    uses :func:`greenback.await_`, the existing portal will take care
    of it.

    Otherwise (if there is no portal already available to the current task),
    :func:`with_portal_run_sync` creates a new portal which lasts only for the
    duration of the call to *sync_fn*.

    This function does *not* add any cancellation point or schedule point
    beyond those that already exist due to any :func:`await_`\\s inside *sync_fn*.
    """

    this_task = current_task()
    if this_task in task_portals:
        return sync_fn(*args, **kwds)
    task_portals[this_task] = greenlet.getcurrent()
    try:
        res: T = await _greenback_shim_sync(partial(sync_fn, *args, **kwds))
        return res
    finally:
        del task_portals[this_task]


if TYPE_CHECKING:
    from trio.abc import Instrument
else:
    Instrument = object


class AutoPortalInstrument(Instrument):
    def __init__(self) -> None:
        # {task: nursery depth at which we'll auto-portalize new children}
        # Rationale for tracking the depth: in
        #     async with trio.open_nursery() as outer:
        #         await with_portal_run_tree(something)
        # we only want to portalize the tasks under `something`, not children
        # spawned into `outer`.
        self.tasks: dict[trio.lowlevel.Task, int] = {}
        self.refs = 0

    def task_spawned(self, task: trio.lowlevel.Task) -> None:
        if task.parent_nursery is None:  # pragma: no cover
            # We shouldn't see the init task (since this instrument is
            # added only after run() starts up) but don't crash if we do.
            return
        parent = task.parent_nursery.parent_task
        depth = self.tasks.get(parent)
        if depth is None:
            return
        if parent.child_nurseries.index(task.parent_nursery) >= depth:
            bestow_portal(task)
            self.tasks[task] = 0

    def task_exited(self, task: trio.lowlevel.Task) -> None:
        self.tasks.pop(task, None)


# We can't initialize this at global scope because we don't want to import Trio
# if we're being used in an asyncio program. It will be initialized on the first
# call to with_portal_run_tree().
instrument_holder: trio.lowlevel.RunVar[AutoPortalInstrument | None] | None = None


async def with_portal_run_tree(
    async_fn: Callable[..., Awaitable[T]], *args: Any, **kwds: Any
) -> T:
    """Execute ``await async_fn(*args, **kwds)`` in a context that allows use
    of :func:`greenback.await_` both in *async_fn* itself and in any tasks
    that are spawned into child nurseries of *async_fn*, recursively.

    You can use this to create an entire Trio run (except system
    tasks) that runs with :func:`greenback.await_` available: say
    ``trio.run(with_portal_run_tree, main)``.

    This function does *not* add any cancellation point or schedule point
    beyond those that already exist inside *async_fn*.

    Availability: Trio only.

    .. note:: The automatic "portalization" of child tasks is
       implemented using a Trio `instrument <trio.abc.Instrument>`,
       which has a small performance impact on task spawning for the
       entire Trio run. To minimize this impact, a single instrument
       is used even if you have multiple :func:`with_portal_run_tree`
       calls running simultaneously, and the instrument will be
       removed as soon as all such calls have completed.

    """
    try:
        import trio

        try:
            from trio import lowlevel as trio_lowlevel
        except ImportError:  # pragma: no cover
            if not TYPE_CHECKING:
                from trio import hazmat as trio_hazmat

        this_task = trio_lowlevel.current_task()
    except Exception:
        raise RuntimeError("This function is only supported when running under Trio")

    global instrument_holder
    if instrument_holder is None:
        instrument_holder = trio_lowlevel.RunVar("greenback_instrument", default=None)
    instrument = instrument_holder.get()
    if instrument is None:
        # We're the only with_portal_run_tree() in this Trio run at the moment -->
        # set up the instrument and store it in the RunVar for other calls to find
        instrument = AutoPortalInstrument()
        trio_lowlevel.add_instrument(instrument)
        instrument_holder.set(instrument)
    elif this_task in instrument.tasks:
        # We're already inside another call to with_portal_run_tree(), so nothing
        # more needs to be done
        assert this_task in task_portals
        return await async_fn(*args, **kwds)

    # Store our current nursery depth. This allows the instrument to
    # distinguish new tasks spawned in child nurseries of async_fn()
    # (which should get auto-portalized) from new tasks spawned in
    # nurseries that enclose this call (which shouldn't, even if they
    # have the same parent task).
    instrument.tasks[this_task] = len(this_task.child_nurseries)
    instrument.refs += 1
    try:
        return await with_portal_run(async_fn, *args, **kwds)
    finally:
        del instrument.tasks[this_task]
        instrument.refs -= 1
        if instrument.refs == 0:
            # There are no more with_portal_run_tree() calls executing
            # in this run, so clean up the instrument.
            instrument_holder.set(None)
            trio_lowlevel.remove_instrument(instrument)


async def adapt_awaitable(aw: Awaitable[T]) -> T:
    return await aw


def await_(aw: Awaitable[T]) -> T:
    """Run an async function or await an awaitable from a synchronous function,
    using the portal set up for the current async task by :func:`ensure_portal`,
    :func:`bestow_portal`, :func:`with_portal_run`, or :func:`with_portal_run_sync`.

    ``greenback.await_(foo())`` is equivalent to ``await foo()``, except that
    the `greenback` version can be written in a synchronous function while
    the native version cannot.
    """
    try:
        task = current_task()
        portal_greenlet = task_portals.get(task, None)
        if portal_greenlet is None:
            if task in task_portals:
                library = sniffio.current_async_library()
                raise RuntimeError(
                    f"You must yield to the event loop (try 'await {library}."
                    "sleep(0)') after calling 'greenback.bestow_portal()' on "
                    "the current task before using the portal"
                )
            else:
                raise RuntimeError(
                    "You must create a greenback portal for this task in order "
                    "to use 'greenback.await_()'. Try 'await "
                    "greenback.ensure_portal()' or "
                    "'await greenback.with_portal_run(some_fn, *args)'."
                )
    except BaseException:
        if isinstance(aw, collections.abc.Coroutine):
            # Suppress the "coroutine was never awaited" warning
            aw.close()
        raise

    # If this is a non-coroutine awaitable, turn it into a coroutine
    if isinstance(aw, collections.abc.Coroutine):
        coro: Coroutine[Any, Any, T] = aw
        trim_tb_frames = 2
    else:
        coro = adapt_awaitable(aw)
        trim_tb_frames = 3

    # Step through the coroutine until it's exhausted, sending each trap
    # into the portal for the event loop to process.
    this_greenlet = greenlet.getcurrent()
    next_send: outcome.Outcome[Any] = outcome.Value(None)
    while True:
        try:
            # next_yield is a Future (under asyncio) or a checkpoint
            # or WaitTaskRescheduled marker (under Trio)
            next_yield: Any = next_send.send(coro)  # type: ignore
        except StopIteration as ex:
            return ex.value  # type: ignore
        except BaseException as ex:
            # Trim internal frames for a nicer traceback.
            # ex.__traceback__ covers the next_send.send(coro) line above;
            # its tb_next is in Value.send() or Error.send();
            # and tb_next of that covers the outermost frame in the user's
            # coroutine, which is what interests us.
            tb = ex.__traceback__
            assert tb is not None
            for _ in range(trim_tb_frames):
                if tb.tb_next is None:
                    # If we get here, there were fewer traceback frames
                    # than we expected, meaning we probably didn't
                    # even make it to the user's code. Don't do any
                    # trimming.
                    raise
                tb = tb.tb_next
            exception_from_greenbacked_function = ex.with_traceback(tb)
            # This line shows up in tracebacks, so give the variable a good name
            raise exception_from_greenbacked_function

        # next_send is an outcome.Outcome representing the value or error
        # with which the event loop wants to resume the task
        next_send = portal_greenlet.switch((next_yield, this_greenlet))
