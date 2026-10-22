package crystil

import (
	"net/http"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/CrystilAI/go-sdk/internal"
)

// providerModules names each provider's Go SDK module without its major
// version suffix, so that any major version matches.
var providerModules = map[Provider]string{
	ProviderOpenAI:    "github.com/openai/openai-go",
	ProviderAnthropic: "github.com/anthropics/anthropic-sdk-go",
	ProviderGoogle:    "google.golang.org/genai",
}

// sdkVersion returns the version of the provider's SDK when that SDK sent the
// request, or "". Each official SDK names its version in a request header, so a
// request from another library reports none, even with the official SDK built in.
func sdkVersion(p Provider, header http.Header) string {
	module, ok := providerModules[p]
	if !ok {
		return ""
	}
	// A program can build in more than one major version of an SDK, as during a
	// migration; the header tells which one sent the request.
	for _, version := range moduleVersions(internal.BuildDeps(), module) {
		if version != "" && headerNames(p, header, version) {
			return version
		}
	}
	return ""
}

// headerNames reports whether the request header names version as the version
// of the provider's SDK that sent the request.
func headerNames(p Provider, header http.Header, version string) bool {
	switch p {
	case ProviderOpenAI, ProviderAnthropic:
		return header.Get("X-Stainless-Package-Version") == version
	case ProviderGoogle:
		for _, value := range header.Values("X-Goog-Api-Client") {
			if slices.Contains(strings.Fields(value), "google-genai-sdk/"+version) {
				return true
			}
		}
	}
	return false
}

// langChainVersion returns the version of langchaingo the program is built
// with. LangChain payloads report it: langchaingo calls providers with its own clients.
func langChainVersion() string {
	return moduleVersion(internal.BuildDeps(), "github.com/tmc/langchaingo")
}

// moduleVersion returns the version of module in deps, at any major version,
// without its "v". It returns "" when the module is missing, replaced by a
// local copy, or built in at more than one major version.
func moduleVersion(deps []*debug.Module, module string) string {
	if versions := moduleVersions(deps, module); len(versions) == 1 {
		return versions[0]
	}
	return ""
}

// moduleVersions returns the version of module in deps at each major version
// built in, without its "v": "" for one replaced by a local copy.
func moduleVersions(deps []*debug.Module, module string) []string {
	var versions []string
	for _, dep := range deps {
		major, ok := strings.CutPrefix(dep.Path, module+"/v")
		if dep.Path != module && (!ok || major == "" || strings.Trim(major, "0123456789") != "") {
			continue
		}
		version := dep.Version
		if dep.Replace != nil {
			version = dep.Replace.Version
		}
		versions = append(versions, strings.TrimPrefix(version, "v"))
	}
	return versions
}
