package bench

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOllamaBase(t *testing.T) {
	cases := map[string]string{
		"":                      "http://127.0.0.1:11434",
		"0.0.0.0":               "http://127.0.0.1:11434",
		"0.0.0.0:8080":          "http://127.0.0.1:8080",
		"http://localhost:1234": "http://localhost:1234",
		"https://box.lan":       "https://box.lan:11434",
	}
	for in, want := range cases {
		if got := OllamaBase(in); got != want {
			t.Errorf("OllamaBase(%q) = %q, want %q", in, got, want)
		}
	}
}

// Real output captured from llama-bench (build 8642541b2) on an M4 Pro.
func TestParseLlamaBench(t *testing.T) {
	data, err := os.ReadFile("testdata/llama-bench-m4pro.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseLlamaBench(data, "x.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if r.Label != "MEASURED" || r.GenTPS < 50 || r.PromptTPS < 100 || r.ParamSize != "3B" || r.Quant != "Q4_K - Medium" {
		t.Fatalf("%+v", r)
	}
	if _, err := ParseLlamaBench([]byte(`[]`), "x"); err == nil {
		t.Fatal("empty output must fail")
	}
}

func fakeOllama(t *testing.T) *Ollama {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0.40.13"}`))
		case "/api/tags":
			w.Write([]byte(`{"models":[
				{"name":"gpt-oss:120b-cloud","size":0,"remote_host":"https://ollama.com:443","details":{"family":"gptoss"}},
				{"name":"nomic-embed-text:latest","size":274000000,"details":{"family":"nomic-bert"}},
				{"name":"llama3.2:latest","size":2019000000,"details":{"family":"llama","parameter_size":"3.2B","quantization_level":"Q4_K_M"}}]}`))
		case "/api/generate":
			w.Write([]byte(`{"eval_count":128,"eval_duration":1000000000,"prompt_eval_count":300,"prompt_eval_duration":500000000,"load_duration":2000000000}`))
		case "/api/ps":
			w.Write([]byte(`{"models":[{"name":"llama3.2:latest","size":2561000078,"size_vram":2561000078}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Ollama{Base: srv.URL, Client: srv.Client()}
}

func TestOllamaBench(t *testing.T) {
	o := fakeOllama(t)
	ctx := context.Background()
	list, err := o.List(ctx)
	if err != nil || len(list) != 3 {
		t.Fatalf("%v %v", list, err)
	}
	if list[0].Benchable() || list[1].Benchable() || !list[2].Benchable() {
		t.Fatalf("cloud and embedding models must not be benchable: %+v", list)
	}
	r, err := o.Bench(ctx, "llama3.2", 2)
	if err != nil {
		t.Fatal(err)
	}
	if r.GenTPS != 128 || r.PromptTPS != 600 || r.GPUShare != 1 || r.Model != "llama3.2:latest" || r.RuntimeVersion != "0.40.13" {
		t.Fatalf("%+v", r)
	}
	if _, err := o.Bench(ctx, "qwen3:8b", 1); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("missing model: %v", err)
	}
	if _, err := o.Bench(ctx, "gpt-oss:120b-cloud", 1); err == nil || !strings.Contains(err.Error(), "cloud") {
		t.Fatalf("cloud model must be refused: %v", err)
	}
}

func TestOllamaDown(t *testing.T) {
	o := &Ollama{Base: "http://127.0.0.1:1", Client: http.DefaultClient}
	if _, err := o.List(context.Background()); !errors.Is(err, ErrNoRuntime) {
		t.Fatalf("%v", err)
	}
}
