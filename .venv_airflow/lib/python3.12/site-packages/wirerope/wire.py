""":mod:`wirerope.wire` --- end-point instant for each bound method
===================================================================
"""
import six
import types
from .callable import Descriptor
from ._compat import functools

__all__ = 'Wire',


@functools.singledispatch
def descriptor_bind(descriptor, obj, type_):
    binder = Descriptor(descriptor).detect_binder(obj, type_)
    return binder(descriptor, obj, type_)


@descriptor_bind.register(types.FunctionType)
def descriptor_bind_function(descriptor, obj, type):
    return obj, obj


class Wire(object):
    """The core data object for each function for bound method.

    Inherit this class to implement your own Wire classes.

    - For normal functions, each function is directly wrapped by **Wire**.
    - For any methods or descriptors (including classmethod, staticmethod),
      each one is wrapped by :class:`wirerope.rope.MethodRopeMixin`
      and it creates **Wire** object for each bound object.
    """

    __slots__ = (
        '_rope', '_callable', '_binding', '__func__', '_owner',
        '_bound_objects')

    def __init__(self, rope, owner, binding):
        self._rope = rope
        self._callable = rope.callable
        self._owner = owner
        self._binding = binding
        if binding:
            func = self._callable.wrapped_object.__get__
            if self._callable.is_property:
                wrapped = functools.partial(func, *binding)
                if six.PY2:
                    # functools.wraps requires those attributes but
                    # py2 functools.partial doesn't have them
                    wrapped.__module__ = owner.__module__
                    wrapped.__name__ = func.__name__
                self.__func__ = wrapped
            else:
                self.__func__ = func(*binding)
        else:
            self.__func__ = self._callable.wrapped_object
        if self._binding is None:
            self._bound_objects = ()
        else:
            _, binder = descriptor_bind(
                self._callable.wrapped_object, *self._binding)
            if binder is not None:
                self._bound_objects = (binder, )
            else:
                self._bound_objects = ()
        assert callable(self.__func__), self.__func__
        if rope._wrapped:
            functools.wraps(self.__func__)(self)

    def _on_property(self):
        return self.__func__()
