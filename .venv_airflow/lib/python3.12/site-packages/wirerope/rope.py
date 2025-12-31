""":mod:`wirerope.rope` --- Wire access dispatcher for descriptor type.
=======================================================================
"""
import six
from .callable import Callable
from .wire import descriptor_bind
from ._compat import functools

__all__ = 'WireRope', 'RopeCore'


class RopeCore(object):
    """The base rope object.

    To change rope behavior, create a subclass or compatible class
    and pass it to `core_class` argument of :class wirerope.rope.WireRope`.
    """

    def __init__(self, callable, rope):
        super(RopeCore, self).__init__()
        self.callable = callable
        self.rope = rope

    @property
    def wire_class(self):
        return self.rope.wire_class


class MethodRopeMixin(object):

    def __init__(self, *args, **kwargs):
        super(MethodRopeMixin, self).__init__(*args, **kwargs)
        assert not self.callable.is_barefunction

    def __set_name__(self, owner, name):
        # Use a non-identifier character as separator to prevent name clash.
        self.wire_name = '|'.join(['__wire', owner.__name__, name])

    def __get__(self, obj, type=None):
        cw = self.callable
        co = cw.wrapped_object
        owner, _ = descriptor_bind(co, obj, type)
        if owner is None:  # invalid binding but still wire it
            owner = obj if obj is not None else type
        if hasattr(self, 'wire_name'):
            wire_name = self.wire_name
            # Lookup in `__dict__` instead of using `getattr`, because
            # `getattr` falls back to class attributes.
            wire = owner.__dict__.get(wire_name)
        else:
            wire_name_parts = ['__wire_', cw.wrapped_callable.__name__]
            if owner is type:
                wire_name_parts.extend(('_', type.__name__))
            wire_name = ''.join(wire_name_parts)
            wire = getattr(owner, wire_name, None)
        if wire is None:
            wire = self.wire_class(self, owner, (obj, type))
            setattr(owner, wire_name, wire)
        assert callable(wire.__func__)
        return wire


class PropertyRopeMixin(object):

    def __init__(self, *args, **kwargs):
        super(PropertyRopeMixin, self).__init__(*args, **kwargs)
        assert not self.callable.is_barefunction

    def __set_name__(self, owner, name):
        # Use a non-identifier character as separator to prevent name clash.
        self.wire_name = '|'.join(['__wire', owner.__name__, name])

    def __get__(self, obj, type=None):
        cw = self.callable
        co = cw.wrapped_object
        owner, _ = descriptor_bind(co, obj, type)
        if owner is None:  # invalid binding but still wire it
            owner = obj if obj is not None else type
        if hasattr(self, 'wire_name'):
            wire_name = self.wire_name
            # Lookup in `__dict__` instead of using `getattr`, because
            # `getattr` falls back to class attributes.
            wire = owner.__dict__.get(wire_name)
        else:
            wire_name_parts = ['__wire_', cw.wrapped_callable.__name__]
            if owner is type:
                wire_name_parts.extend(('_', type.__name__))
            wire_name = ''.join(wire_name_parts)
            wire = getattr(owner, wire_name, None)
        if wire is None:
            wire = self.wire_class(self, owner, (obj, type))
            setattr(owner, wire_name, wire)
        return wire._on_property()  # requires property path


class FunctionRopeMixin(object):

    def __init__(self, *args, **kwargs):
        super(FunctionRopeMixin, self).__init__(*args, **kwargs)
        assert self.callable.is_barefunction or self.callable.is_boundmethod
        self._wire = self.wire_class(self, None, None)

    def __getattr__(self, name):
        try:
            return self.__getattribute__(name)
        except AttributeError:
            pass
        return getattr(self._wire, name)


class CallableRopeMixin(object):

    def __init__(self, *args, **kwargs):
        super(CallableRopeMixin, self).__init__(*args, **kwargs)
        self.__call__ = functools.wraps(self.callable.wrapped_object)(self)

    def __call__(self, *args, **kwargs):
        return self._wire(*args, **kwargs)


class WireRope(object):
    """The end-user wrapper for callables.

    Any callable can be wrapped by this class regardless of its concept -
    free function, method, property or even more weird one.
    The calling type is decided by each call and redirected to proper
    RopeMixin.

    The rope will detect method or property owner by needs. It also will return
    or call their associated `wirerope.wire.Wire` object - which are the user
    defined behavior.
    """

    def __init__(
            self, wire_class, core_class=RopeCore,
            wraps=False, rope_args=None):
        self.wire_class = wire_class
        self.method_rope = type(
            '_MethodRope', (MethodRopeMixin, core_class), {})
        self.property_rope = type(
            '_PropertyRope', (PropertyRopeMixin, core_class), {})
        self.function_rope = type(
            '_FunctionRope', (FunctionRopeMixin, core_class), {})
        self.callable_function_rope = type(
            '_CallableFunctionRope',
            (CallableRopeMixin, FunctionRopeMixin, core_class), {})
        for rope in (self, self.method_rope, self.property_rope,
                     self.function_rope, self.callable_function_rope):
            rope._wrapped = wraps
            rope._args = rope_args

    def __call__(self, function):
        """Wrap a function/method definition.

        :return: Rope object. The return type is up to given callable is
                 function, method or property.
        """
        cw = Callable(function)
        if cw.is_barefunction or cw.is_boundmethod:
            rope_class = self.callable_function_rope
            wire_class_call = self.wire_class.__call__
            if six.PY3:
                if wire_class_call.__qualname__ == 'type.__call__':
                    rope_class = self.function_rope
            else:
                # method-wrapper test for CPython2.7
                # im_class == type test for PyPy2.7
                if type(wire_class_call).__name__ == 'method-wrapper' or \
                        wire_class_call.im_class == type:
                    rope_class = self.function_rope
        elif cw.is_property:
            rope_class = self.property_rope
        else:
            rope_class = self.method_rope
        rope = rope_class(cw, rope=self)
        if rope._wrapped:
            rope = functools.wraps(cw.wrapped_callable)(rope)
        return rope
