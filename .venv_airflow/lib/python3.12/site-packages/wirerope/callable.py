from __future__ import absolute_import

import types
import six
from ._util import cached_property
from ._compat import inspect

__all__ = ('Callable', )


_inspect_iscoroutinefunction = getattr(
    inspect, 'iscoroutinefunction', lambda f: False)


class _Reagent(object):
    pass


_reagent = _Reagent()


def _f(owner):
    return owner


def _name_binder(descriptor, obj, type):
    return type, None


def _type_binder(descriptor, obj, type):
    return type, type


def _obj_binder(descriptor, obj, type):
    return obj, obj


_descriptor_binder_cache = {}


class Descriptor(object):

    def __init__(self, descriptor):
        self.descriptor = descriptor
        self.descriptor_class = type(descriptor)

    def detect_function_attr_name(self):
        indicator = object()
        descriptor = self.descriptor_class(indicator)
        for name in dir(descriptor):
            try:
                attr = getattr(descriptor, name)
            except AttributeError:
                continue
            if attr is indicator:
                # detected!
                return name
        else:
            raise RuntimeError(
                "The given function doesn't hold the given function as an "
                "attribute. Is it a correct descriptor?")

    def detect_property(self, obj, type_):
        d = self.descriptor_class(_f)
        method_or_value = d.__get__(obj, type_)
        return method_or_value is obj or method_or_value is type_

    def detect_binder(self, obj, type_):
        key = (self.descriptor_class, obj is not None)
        if key not in _descriptor_binder_cache:
            d = self.descriptor_class(_f)
            method = d.__get__(obj, type_)
            if isinstance(method, types.FunctionType):
                # not a boundmethod - probably staticmethod
                binder = _name_binder
            elif method is obj:
                binder = _obj_binder
            elif method is type_:
                binder = _type_binder
            elif callable(method):
                owner = method()
                if owner is type_:
                    binder = _type_binder
                elif owner is obj:
                    binder = _obj_binder
                else:
                    binder = None
            elif method is d:
                # some non-method descriptor
                binder = _name_binder
            else:
                binder = None
            if binder is None:
                raise TypeError(
                    "'descriptor_bind' fails to auto-detect binding rule "
                    "of the given descriptor. Specify the rule by "
                    "'wirerope.wire.descriptor_bind.register'.")
            _descriptor_binder_cache[key] = binder
        else:
            binder = _descriptor_binder_cache[key]
        return binder


class Callable(object):
    """A wrapper object including more information of callables."""

    def __init__(self, f):
        self.wrapped_object = f
        self.is_function_type = type(self) is types.FunctionType  # noqa
        if self.is_descriptor:
            self.descriptor = Descriptor(f)
            f = getattr(f, self.descriptor.detect_function_attr_name())
        else:
            self.descriptor = None
        self.wrapped_callable = f
        self.is_wrapped_coroutine = getattr(f, '_is_coroutine', None)
        self.is_coroutine = self.is_wrapped_coroutine or \
            _inspect_iscoroutinefunction(f)

    @cached_property
    def signature(self):
        return inspect.signature(self.wrapped_callable)

    @cached_property
    def parameters(self):
        return list(self.signature.parameters.values())

    @property
    def first_parameter(self):
        return self.parameters[0] if self.parameters else None

    @cached_property
    def is_boundmethod(self):
        if self.is_function_type or self.is_builtin_property:
            return False
        new_bound = self.wrapped_object.__get__(object())
        try:
            if six.PY2:
                return new_bound is self.wrapped_object
            else:
                return type(new_bound) is type(self.wrapped_object)  # noqa
        except Exception:
            return False

    if six.PY2:
        @property
        def is_unboundmethod(self):
            return type(self.wrapped_object) is type(Callable.__init__)  # noqa

    @cached_property
    def is_descriptor(self):
        if self.is_boundmethod:
            return False
        is_descriptor = type(self.wrapped_object).__get__ \
            is not types.FunctionType.__get__  # noqa
        if six.PY2:
            is_descriptor = is_descriptor and \
                not (self.is_unboundmethod or self.is_boundmethod)
        return is_descriptor

    @cached_property
    def is_builtin_property(self):
        return issubclass(type(self.wrapped_object), property)

    @cached_property
    def is_property(self):
        return self.is_builtin_property or \
            (self.is_descriptor and self.descriptor.detect_property(
                _reagent, _Reagent))

    if six.PY34:
        @cached_property
        def is_barefunction(self):
            cc = self.wrapped_callable
            method_name = cc.__qualname__.split('<locals>.')[-1]
            if method_name == cc.__name__:
                return True
            return False
    else:
        @cached_property
        def is_barefunction(self):
            # im_class does not exist at this point
            if self.is_descriptor:
                return False
            if six.PY2:
                if self.is_unboundmethod:
                    return False
            return not (self.is_membermethod or self.is_classmethod)

    @cached_property
    def is_member(self):
        """Test given argument is a method or not.

        :rtype: bool

        :note: The test is partially based on the first parameter name.
            The test result might be wrong.
        """
        if six.PY34:
            if self.is_barefunction:
                return False
            if not self.is_descriptor:
                return True
        return self.first_parameter is not None \
            and self.first_parameter.name == 'self'

    @cached_property
    def is_membermethod(self):
        """Test given argument is a method or not.

        :rtype: bool

        :note: The test is partially based on the first parameter name.
            The test result might be wrong.
        """
        if self.is_boundmethod:
            return False
        if self.is_property:
            return False
        return self.is_member

    @cached_property
    def is_classmethod(self):
        """Test given argument is a classmethod or not.

        :rtype: bool

        :note: The test is partially based on the first parameter name.
            The test result might be wrong.
        """
        if isinstance(self.wrapped_object, classmethod):
            return True
        if six.PY34:
            if self.is_barefunction:
                return False
            if not self.is_descriptor:
                return False

        return self.first_parameter is not None \
            and self.first_parameter.name == 'cls'
