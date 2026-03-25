// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.List;
import java.util.function.Function;

/**
 * Result type for error handling without exceptions.
 * Either Ok(value) or Err(errors).
 */
public sealed interface Result<T> {

    record Ok<T>(T value) implements Result<T> {}
    record Err<T>(List<String> errors) implements Result<T> {}

    default boolean isOk() { return this instanceof Ok; }
    default boolean isErr() { return this instanceof Err; }

    default T unwrap() {
        return switch (this) {
            case Ok<T> ok -> ok.value();
            case Err<T> err -> throw new IllegalStateException("unwrap on Err: " + err.errors());
        };
    }

    default T or(T fallback) {
        return switch (this) {
            case Ok<T> ok -> ok.value();
            case Err<T> _ -> fallback;
        };
    }

    default <U> Result<U> map(Function<T, U> fn) {
        return switch (this) {
            case Ok<T> ok -> new Ok<>(fn.apply(ok.value()));
            case Err<T> err -> new Err<>(err.errors());
        };
    }

    default <U> Result<U> flatMap(Function<T, Result<U>> fn) {
        return switch (this) {
            case Ok<T> ok -> fn.apply(ok.value());
            case Err<T> err -> new Err<>(err.errors());
        };
    }

    static <T> Result<T> ok(T value) { return new Ok<>(value); }
    static <T> Result<T> err(String error) { return new Err<>(List.of(error)); }
    static <T> Result<T> err(List<String> errors) { return new Err<>(errors); }
}
