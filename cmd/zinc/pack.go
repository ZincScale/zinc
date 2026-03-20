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

package main

import "fmt"

// runPack packages a Zinc project for deployment.
// TODO: Integrate with Mill for native-image, JLink, Docker packaging.
func runPack(target, format string) {
	fmt.Println("zinc pack is not yet implemented for Java target.")
	fmt.Println("Use Mill directly:")
	fmt.Println("  mill app.nativeImage    # GraalVM native binary")
	fmt.Println("  mill app.jlink          # self-contained JRE")
	fmt.Println("  mill app.docker         # Docker image")
}
