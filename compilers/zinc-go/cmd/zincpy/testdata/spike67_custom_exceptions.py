# Custom exception subclasses: message-based, registered into the runtime
# exception hierarchy so base-class except clauses match.


class AppError(Exception):
    pass


class ValidationError(AppError):
    pass


class ConfigError(AppError):
    pass


def validate(name: str) -> str:
    if name == "":
        raise ValidationError("name must not be empty")
    if name == "?":
        raise ConfigError("bad config")
    return name


for n in ["alice", "", "?"]:
    try:
        print("ok:", validate(n))
    except ValidationError as e:
        print("validation error:", e)
    except ConfigError as e:
        print("config error:", e)


# A subclass is caught by its base class.
try:
    raise ValidationError("subclass caught by base")
except AppError as e:
    print("AppError caught:", e)


# ... and by Exception / BaseException.
try:
    raise ConfigError("caught generically")
except Exception as e:
    print("Exception caught:", e)


# Exact-type dispatch picks the matching arm, not an earlier sibling.
try:
    raise ConfigError("pick me")
except ValidationError:
    print("wrong arm")
except ConfigError as e:
    print("right arm:", e)


# No-argument raise yields an empty message (matches str(Exc())).
class EmptyError(Exception):
    pass


try:
    raise EmptyError()
except EmptyError as e:
    print("empty message:[" + str(e) + "]")


# Re-raise inside a handler propagates to an outer try.
def risky() -> None:
    try:
        raise ValidationError("inner")
    except ValidationError:
        print("logging, re-raising")
        raise


try:
    risky()
except AppError as e:
    print("outer caught:", e)
