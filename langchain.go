package crystil

import (
	"runtime"
	"strings"
)

// langChainLLMs maps each langchaingo LLM package to the title the other SDKs
// give the same LangChain model class, such as ChatOpenAI's "chatopenai", and the
// provider it calls. Other packages, such as llms/anthropic, which no Crystil SDK
// supports, stay unlabeled.
var langChainLLMs = map[string]struct {
	title    string
	provider Provider
}{
	"github.com/tmc/langchaingo/llms/openai":          {"chatopenai", ProviderOpenAI},
	"github.com/tmc/langchaingo/llms/googleai":        {"chatgooglegenai", ProviderGoogle},
	"github.com/tmc/langchaingo/llms/googleai/vertex": {"chatvertexai", ProviderGoogle},
}

// langChainTitle returns the LangChain title for a request to p, or "" when no
// langchaingo LLM package is on the call stack: langchaingo marks its requests in
// no other way.
func langChainTitle(p Provider) string {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		frame, more := frames.Next()
		pkg := funcPackage(frame.Function)
		if i := strings.Index(pkg, "/internal/"); i >= 0 {
			pkg = pkg[:i]
		}
		if llm, ok := langChainLLMs[pkg]; ok {
			// An LLM that calls another provider sent no request to p: the caller
			// made it from inside a langchaingo callback, such as a StreamingFunc.
			if llm.provider != p {
				return ""
			}
			return llm.title
		}
		if !more {
			return ""
		}
	}
}

// funcPackage returns the package path of a function name such as
// "example.com/pkg.(*T).Method".
func funcPackage(name string) string {
	slash := strings.LastIndex(name, "/")
	if dot := strings.Index(name[slash+1:], "."); dot >= 0 {
		return name[:slash+1+dot]
	}
	return name
}
