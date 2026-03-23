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

package typechecker

import (
	"os/exec"
	"strings"
	"sync"
)

// JavaTypeResolver resolves method return types for Java standard library
// and third-party classes using javap. Results are cached.
type JavaTypeResolver struct {
	cache map[string]*JavaClassInfo
	mu    sync.Mutex
}

// JavaClassInfo holds parsed method signatures for a Java class.
type JavaClassInfo struct {
	Name       string            // e.g. "java.util.concurrent.ArrayBlockingQueue"
	TypeParams []string          // e.g. ["E"]
	Methods    []JavaMethodSig   // public methods
}

// JavaMethodSig represents a parsed Java method signature.
type JavaMethodSig struct {
	Name       string   // e.g. "poll"
	ReturnType string   // e.g. "E", "int", "boolean", "void"
	ParamTypes []string // e.g. ["long", "java.util.concurrent.TimeUnit"]
}

var globalResolver = &JavaTypeResolver{cache: make(map[string]*JavaClassInfo)}

// ResolveMethodReturn looks up the return type of a method call.
// className is the fully-qualified Java class (e.g. "java.util.concurrent.ArrayBlockingQueue")
// typeArgs are the concrete generic args (e.g. ["FlowFile"])
// methodName is the method being called (e.g. "poll")
// Returns the resolved return type, or "" if unknown.
func ResolveMethodReturn(className string, typeArgs []string, methodName string) string {
	return globalResolver.resolveMethodReturn(className, typeArgs, methodName)
}

func (r *JavaTypeResolver) resolveMethodReturn(className string, typeArgs []string, methodName string) string {
	info := r.getClassInfo(className)
	if info == nil {
		return ""
	}

	// Find matching method
	for _, m := range info.Methods {
		if m.Name == methodName {
			ret := m.ReturnType
			// Resolve generic type params: E → typeArgs[0], etc.
			for i, tp := range info.TypeParams {
				if ret == tp && i < len(typeArgs) {
					return typeArgs[i]
				}
			}
			return ret
		}
	}
	return ""
}

func (r *JavaTypeResolver) getClassInfo(className string) *JavaClassInfo {
	r.mu.Lock()
	defer r.mu.Unlock()

	if info, ok := r.cache[className]; ok {
		return info
	}

	info := parseJavap(className)
	r.cache[className] = info
	return info
}

// parseJavap runs javap -public on a class and parses the output.
func parseJavap(className string) *JavaClassInfo {
	cmd := exec.Command("javap", "-public", className)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	info := &JavaClassInfo{Name: className}
	lines := strings.Split(string(out), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse class declaration for type params: "public class Foo<E> extends ..."
		if strings.HasPrefix(line, "public class ") || strings.HasPrefix(line, "public abstract class ") {
			if idx := strings.Index(line, "<"); idx != -1 {
				end := strings.Index(line[idx:], ">")
				if end != -1 {
					params := line[idx+1 : idx+end]
					for _, p := range strings.Split(params, ",") {
						p = strings.TrimSpace(p)
						// Handle bounded types: "E extends Comparable<E>" → "E"
						if spaceIdx := strings.Index(p, " "); spaceIdx != -1 {
							p = p[:spaceIdx]
						}
						info.TypeParams = append(info.TypeParams, p)
					}
				}
			}
			continue
		}

		// Parse method: "public E poll(long, java.util.concurrent.TimeUnit) throws ..."
		if strings.HasPrefix(line, "public ") && strings.Contains(line, "(") && !strings.Contains(line, " class ") {
			sig := parseMethodLine(line)
			if sig != nil {
				info.Methods = append(info.Methods, *sig)
			}
		}
	}

	return info
}

// parseMethodLine parses a javap method line like:
// "public E poll(long, java.util.concurrent.TimeUnit) throws java.lang.InterruptedException;"
func parseMethodLine(line string) *JavaMethodSig {
	// Remove "public " prefix and trailing ";"
	line = strings.TrimPrefix(line, "public ")
	line = strings.TrimSuffix(line, ";")

	// Remove "throws ..." clause
	if idx := strings.Index(line, " throws "); idx != -1 {
		line = line[:idx]
	}

	// Remove "static ", "final ", "abstract ", "synchronized " modifiers
	for _, mod := range []string{"static ", "final ", "abstract ", "synchronized "} {
		line = strings.TrimPrefix(line, mod)
	}

	// Find the parameter list
	parenOpen := strings.Index(line, "(")
	parenClose := strings.LastIndex(line, ")")
	if parenOpen == -1 || parenClose == -1 {
		return nil
	}

	// Everything before "(" is "ReturnType methodName" or just "ClassName" (constructor)
	beforeParen := line[:parenOpen]
	parts := strings.Fields(beforeParen)
	if len(parts) < 2 {
		// Constructor — skip
		return nil
	}

	// Handle generic method type params: "<T> T[] toArray(T[])" → skip the <T> part
	startIdx := 0
	if strings.HasPrefix(parts[0], "<") {
		startIdx = 1
		// Find the closing >
		for i, p := range parts {
			if strings.HasSuffix(p, ">") {
				startIdx = i + 1
				break
			}
		}
	}

	if startIdx+1 >= len(parts) {
		return nil
	}

	returnType := parts[startIdx]
	methodName := parts[startIdx+1]

	// Parse parameters
	paramStr := line[parenOpen+1 : parenClose]
	var paramTypes []string
	if paramStr != "" {
		for _, p := range strings.Split(paramStr, ",") {
			p = strings.TrimSpace(p)
			// Remove generic wildcard details: "? extends E" → keep as-is
			paramTypes = append(paramTypes, p)
		}
	}

	return &JavaMethodSig{
		Name:       methodName,
		ReturnType: returnType,
		ParamTypes: paramTypes,
	}
}

// Zinc type name → Java fully qualified class name
var zincToJavaClass = map[string]string{
	"Channel":   "java.util.concurrent.ArrayBlockingQueue",
	"List":      "java.util.List",
	"ArrayList": "java.util.ArrayList",
	"Map":       "java.util.Map",
	"HashMap":   "java.util.HashMap",
	"Set":       "java.util.Set",
	"HashSet":   "java.util.HashSet",
	"String":    "java.lang.String",
}

// ResolveZincMethodReturn resolves a method return type for a Zinc type.
// zincType is the Zinc type name (e.g. "Channel"), typeArgs are generic args,
// methodName is the method being called.
func ResolveZincMethodReturn(zincType string, typeArgs []string, methodName string) string {
	javaClass, ok := zincToJavaClass[zincType]
	if !ok {
		return ""
	}
	return ResolveMethodReturn(javaClass, typeArgs, methodName)
}
