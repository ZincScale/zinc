package typechecker

// ResolveZincMethodReturn resolves method return types for Go standard library types.
// This is used by the type checker to infer types through method chains.
func ResolveZincMethodReturn(typeName string, typeArgs []string, method string) string {
	key := typeName + "." + method

	// String methods
	switch key {
	case "String.upper", "String.lower", "String.trim", "String.trimStart", "String.trimEnd",
		"String.replace", "String.substring", "String.repeat":
		return "String"
	case "String.length", "String.indexOf":
		return "int"
	case "String.contains", "String.startsWith", "String.endsWith", "String.isEmpty":
		return "boolean"
	case "String.charAt":
		return "char"
	case "String.split":
		return "String[]"
	}

	// List methods
	if typeName == "List" {
		elemType := "any"
		if len(typeArgs) > 0 {
			elemType = typeArgs[0]
		}
		switch method {
		case "size":
			return "int"
		case "get":
			return elemType
		case "contains":
			return "boolean"
		case "filter", "map", "distinct", "limit", "skip", "sortBy":
			return typeName
		case "sum", "count":
			return "int"
		case "findFirst", "min", "max":
			return elemType
		case "anyMatch", "allMatch", "noneMatch":
			return "boolean"
		case "isEmpty":
			return "boolean"
		}
	}

	// Map methods
	if typeName == "Map" {
		switch method {
		case "size":
			return "int"
		case "containsKey":
			return "boolean"
		case "isEmpty":
			return "boolean"
		}
	}

	return ""
}

// ZincToJavaClass is a no-op stub for Go backend (no Java class mapping needed).
func ZincToJavaClass(name string) (string, bool) {
	return "", false
}

// MethodThrows is a no-op stub for Go backend (Go uses error returns, not exceptions).
func MethodThrows(className, methodName string) (bool, bool) {
	return false, false
}
