""":mod:`wirerope` --- Universal method/function wrapper.
=========================================================
"""

# this line must be placed first for setup.cfg
from ._version import __version__
from .wire import Wire
from .rope import WireRope, RopeCore

__all__ = ('__version__', 'Wire', 'RopeCore', 'WireRope')
