#    Copyright 2017-2022 Alexey Stepanov aka penguinolog

#    Licensed under the Apache License, Version 2.0 (the "License"); you may
#    not use this file except in compliance with the License. You may obtain
#    a copy of the License at

#         http://www.apache.org/licenses/LICENSE-2.0

#    Unless required by applicable law or agreed to in writing, software
#    distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
#    WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
#    License for the specific language governing permissions and limitations
#    under the License.

"""JSONField implementation for SQLAlchemy."""

from __future__ import annotations

import json
import typing

import sqlalchemy.ext.mutable
import sqlalchemy.types

if typing.TYPE_CHECKING:
    import types

    from sqlalchemy.engine import Dialect
    from sqlalchemy.sql.type_api import TypeEngine

__all__ = ("JSONField", "mutable_json_field")


# noinspection PyAbstractClass
class JSONField(sqlalchemy.types.TypeDecorator):  # type: ignore[type-arg]  # pylint: disable=abstract-method
    """Represent an immutable structure as a json-encoded string or json.

    Usage::

        JSONField(enforce_string=True|False, enforce_unicode=True|False)

    """

    def process_literal_param(self, value: typing.Any, dialect: Dialect) -> typing.Any:
        """Re-use of process_bind_param.

        :return: encoded value if required
        :rtype: typing.Union[str, typing.Any]
        """
        return self.process_bind_param(value, dialect)

    impl = sqlalchemy.types.TypeEngine  # Special placeholder
    cache_ok = False  # Cache complexity due to requerement of value re-serialization and mutability

    def __init__(  # pylint: disable=keyword-arg-before-vararg
        self,
        enforce_string: bool = False,
        enforce_unicode: bool = False,
        json: types.ModuleType | typing.Any = json,  # pylint: disable=redefined-outer-name
        json_type: TypeEngine[typing.Any] | type[TypeEngine[typing.Any]] = sqlalchemy.JSON,
        *args: typing.Any,
        **kwargs: typing.Any,
    ) -> None:
        """JSONField.

        :param enforce_string: enforce String(UnicodeText) type usage
        :type enforce_string: bool
        :param enforce_unicode: do not encode non-ascii data
        :type enforce_unicode: bool
        :param json: JSON encoding/decoding library. By default: standard json package.
        :param json_type: the sqlalchemy/dialect class that will be used to render the DB JSON type.
                          By default: sqlalchemy.JSON
        :param args: extra baseclass arguments
        :type args: typing.Any
        :param kwargs: extra baseclass keyworded arguments
        :type kwargs: typing.Any
        """
        self.__enforce_string = enforce_string
        self.__enforce_unicode = enforce_unicode
        self.__json_codec = json
        self.__json_type = json_type
        super().__init__(*args, **kwargs)

    def __use_json(self, dialect: Dialect) -> bool:
        """Helper to determine, which encoder to use.

        :return: use engine-based json encoder
        :rtype: bool
        """
        return hasattr(dialect, "_json_serializer") and not self.__enforce_string

    def load_dialect_impl(self, dialect: Dialect) -> TypeEngine[typing.Any]:
        """Select impl by dialect.

        :return: dialect implementation depends on decoding method
        :rtype: TypeEngine
        """
        # types are handled by DefaultDialect, Dialect class is abstract
        if self.__use_json(dialect):
            return dialect.type_descriptor(self.__json_type)  # type: ignore[arg-type]
        return dialect.type_descriptor(sqlalchemy.UnicodeText)  # type: ignore[arg-type]

    def process_bind_param(self, value: typing.Any, dialect: Dialect) -> str | typing.Any:
        """Encode data, if required.

        :return: encoded value if required
        :rtype: typing.Union[str, typing.Any]
        """
        if self.__use_json(dialect) or value is None:
            return value

        return self.__json_codec.dumps(value, ensure_ascii=not self.__enforce_unicode)

    def process_result_value(self, value: str | typing.Any, dialect: Dialect) -> typing.Any:
        """Decode data, if required.

        :return: decoded result value if required
        :rtype: typing.Any
        """
        if self.__use_json(dialect) or value is None:
            return value

        return self.__json_codec.loads(value)


def mutable_json_field(  # pylint: disable=keyword-arg-before-vararg, redefined-outer-name
    enforce_string: bool = False,
    enforce_unicode: bool = False,
    json: types.ModuleType | typing.Any = json,
    *args: typing.Any,
    **kwargs: typing.Any,
) -> JSONField:
    """Mutable JSONField creator.

    :param enforce_string: enforce String(UnicodeText) type usage
    :type enforce_string: bool
    :param enforce_unicode: do not encode non-ascii data
    :type enforce_unicode: bool
    :param json: JSON encoding/decoding library.
                 By default: standard json package.
    :param args: extra baseclass arguments
    :type args: typing.Any
    :param kwargs: extra baseclass keyworded arguments
    :type kwargs: typing.Any
    :return: Mutable JSONField via MutableDict.as_mutable
    :rtype: JSONField
    """
    return sqlalchemy.ext.mutable.MutableDict.as_mutable(  # type: ignore[return-value]
        JSONField(  # type: ignore[misc]
            enforce_string=enforce_string,
            enforce_unicode=enforce_unicode,
            json=json,
            *args,  # noqa: B026
            **kwargs,
        )
    )
