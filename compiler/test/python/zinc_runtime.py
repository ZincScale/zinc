"""Zinc runtime support for Python 3.14t target."""


class ZincError(Exception):
    """Base exception for Zinc error values.

    Maps to: return Error("msg") in Zinc source.
    """

    def __init__(self, message=None):
        if isinstance(message, Exception):
            super().__init__(str(message))
            self.__cause__ = message
        else:
            super().__init__(message)
