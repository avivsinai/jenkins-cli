from __future__ import absolute_import

import six

import functools
if not hasattr(functools, 'singledispatch'):
    import singledispatch
    functools.singledispatch = singledispatch.singledispatch

if six.PY3:
    import inspect
else:
    import inspect2 as inspect  # noqa
