""":mod:`methodtools` --- functools for methods
===============================================

Expand functools features to methods, classmethods, staticmethods and even for
(unofficial) hybrid methods.

For now, methodtools only provides :func:`methodtools.lru_cache`.

Use methodtools module instead of functools module. Than it will work as you
expected - cache for each bound method.

.. code-block:: python

    from methodtools import lru_cache

    class A(object):

        # cached method. the storage lifetime follows `self` object
        @lru_cache()
        def cached_method(self, args):
            ...

        # cached classmethod. the storage lifetime follows `A` class
        @lru_cache()  # the order is important!
        @classmethod  # always lru_cache on top of classmethod
        def cached_classmethod(self, args):
            ...

        # cached staticmethod. the storage lifetime follows `A` class
        @lru_cache()  # the order is important!
        @staticmethod  # always lru_cache on top of staticmethod
        def cached_staticmethod(self, args):
            ...

    @lru_cache()  # just same as functools.lru_cache
    def cached_function():
        ...
"""

import functools
from wirerope import Wire, WireRope

__version__ = '0.4.7'
__all__ = 'lru_cache',


if hasattr(functools, 'lru_cache'):
    _functools_lru_cache = functools.lru_cache
else:
    try:
        import functools32
    except ImportError:
        # raise AttributeError about fallback failure
        functools.lru_cache  # install `functools32` to run on py2
    else:
        _functools_lru_cache = functools32.lru_cache


class _LruCacheWire(Wire):

    def __init__(self, rope, *args, **kwargs):
        super(_LruCacheWire, self).__init__(rope, *args, **kwargs)
        lru_args, lru_kwargs = rope._args
        wrapper = _functools_lru_cache(
            *lru_args, **lru_kwargs)(self.__func__)
        self.__call__ = wrapper
        self.cache_clear = wrapper.cache_clear
        self.cache_info = wrapper.cache_info

    def __call__(self, *args, **kwargs):
        # descriptor detection support - never called
        return self.__call__(*args, **kwargs)

    def _on_property(self):
        return self.__call__()


@functools.wraps(_functools_lru_cache)
def lru_cache(*args, **kwargs):
    return WireRope(_LruCacheWire, wraps=True, rope_args=(args, kwargs))
