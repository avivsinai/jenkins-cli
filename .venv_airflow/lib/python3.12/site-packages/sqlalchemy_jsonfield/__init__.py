#    Copyright 2016-2022 Alexey Stepanov aka penguinolog
#    Licensed under the Apache License, Version 2.0 (the "License"); you may
#    not use this file except in compliance with the License. You may obtain
#    a copy of the License at

#         http://www.apache.org/licenses/LICENSE-2.0

#    Unless required by applicable law or agreed to in writing, software
#    distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
#    WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
#    License for the specific language governing permissions and limitations
#    under the License.

"""Implement JSONField for SQLAlchemy."""

from __future__ import annotations

# Local Implementation
from .jsonfield import JSONField
from .jsonfield import mutable_json_field

try:  # noqa: SIM105,FURB107,RUF100
    # Local Implementation
    from ._version import version as __version__
except ImportError:
    pass


__all__ = ("JSONField", "mutable_json_field", "__version__")

__author__ = "Alexey Stepanov <penguinolog@gmail.com>"
__author_email__ = "penguinolog@gmail.com"
__url__ = "https://github.com/penguinolog/sqlalchemy_jsonfield"
__description__ = "SQLALchemy JSONField implementation for storing dicts at SQL"
__license__ = "Apache License, Version 2.0"
