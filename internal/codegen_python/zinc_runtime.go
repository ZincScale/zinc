// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package codegen_python

// ZincResultRuntime is the minimal Python runtime for Result[T] / Err handling.
// Inlined into generated code when Result types are used.
const ZincResultRuntime = `class _Ok:
    __slots__ = ('value',)
    def __init__(self, value):
        self.value = value
    def is_ok(self): return True
    def is_err(self): return False
    def unwrap(self): return self.value
    def __repr__(self): return f"Ok({self.value!r})"

class _Err:
    __slots__ = ('error',)
    def __init__(self, error):
        self.error = error
    def is_ok(self): return False
    def is_err(self): return True
    def unwrap(self): raise ValueError(f"called unwrap on Err: {self.error}")
    def __repr__(self): return f"Err({self.error!r})"

def Ok(value): return _Ok(value)
def Err(error): return _Err(error)
`
