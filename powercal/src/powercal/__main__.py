"""Enable ``python -m powercal`` as an entry point equivalent to the ``powercal`` console script."""

from .cli import main

raise SystemExit(main())
