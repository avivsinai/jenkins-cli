"""Top-level package for greenback."""

# Redundant symbol imports flag as public for type checkers
from ._version import __version__ as __version__
from ._impl import (
    ensure_portal as ensure_portal,
    bestow_portal as bestow_portal,
    has_portal as has_portal,
    with_portal_run as with_portal_run,
    with_portal_run_sync as with_portal_run_sync,
    with_portal_run_tree as with_portal_run_tree,
    await_ as await_,
)
from ._util import (
    autoawait as autoawait,
    decorate_as_sync as decorate_as_sync,
    async_context as async_context,
    async_iter as async_iter,
)
