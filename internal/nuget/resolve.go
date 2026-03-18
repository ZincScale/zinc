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

package nuget

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VersionIndex is the response from the NuGet flat container API.
type VersionIndex struct {
	Versions []string `json:"versions"`
}

// DefaultSource is the public NuGet API.
const DefaultSource = "https://api.nuget.org/v3-flatcontainer"

// ResolveLatest queries NuGet for the latest stable version of a package.
// Uses the default public NuGet source.
func ResolveLatest(packageName string) (string, error) {
	return ResolveLatestFrom(DefaultSource, packageName)
}

// ResolveLatestFrom queries a specific NuGet source for the latest stable version.
// The source should be a flat container URL (e.g. https://api.nuget.org/v3-flatcontainer).
func ResolveLatestFrom(source, packageName string) (string, error) {
	url := fmt.Sprintf("%s/%s/index.json",
		strings.TrimRight(source, "/"), strings.ToLower(packageName))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to query NuGet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return "", fmt.Errorf("package %q not found on NuGet", packageName)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("NuGet returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading NuGet response: %w", err)
	}

	var index VersionIndex
	if err := json.Unmarshal(body, &index); err != nil {
		return "", fmt.Errorf("parsing NuGet response: %w", err)
	}

	if len(index.Versions) == 0 {
		return "", fmt.Errorf("no versions found for %q", packageName)
	}

	// Find latest stable version (no prerelease tags)
	latest := ""
	for i := len(index.Versions) - 1; i >= 0; i-- {
		v := index.Versions[i]
		if !isPrerelease(v) {
			latest = v
			break
		}
	}

	if latest == "" {
		// Fall back to latest version even if prerelease
		latest = index.Versions[len(index.Versions)-1]
	}

	return latest, nil
}

// isPrerelease checks if a version string contains prerelease identifiers.
func isPrerelease(version string) bool {
	return strings.Contains(version, "-")
}
