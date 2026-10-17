package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"example.com/nonprofit-media-pipeline/internal/infrai"
	"example.com/nonprofit-media-pipeline/internal/nonprofit"
)

const maxUpload = 20 << 20

type server struct{ pipeline nonprofit.Pipeline }

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	bucket := envOr("INFRAI_BUCKET", "nonprofit-converted-media")
	client := infrai.New(apiKey)
	setupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.EnsureBucket(setupCtx, bucket); err != nil {
		log.Fatalf("create storage bucket: %v", err)
	}

	s := server{pipeline: nonprofit.Pipeline{Store: client, Bucket: bucket}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /uploads", s.upload)
	httpServer := &http.Server{Addr: envOr("LISTEN_ADDR", ":8080"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("nonprofit media service listening on %s", httpServer.Addr)
	log.Fatal(httpServer.ListenAndServe())
}

func (s server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart upload"})
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
		return
	}
	defer file.Close()
	image, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read image"})
		return
	}
	record, err := s.pipeline.Process(r.Context(), nonprofit.Upload{
		Reference: r.FormValue("reference"), Kind: nonprofit.AssetKind(r.FormValue("kind")), Filename: header.Filename, Image: image,
	})
	if err != nil {
		status := http.StatusBadRequest
		var apiErr *infrai.Error
		if errors.As(err, &apiErr) {
			status = http.StatusBadGateway
			if apiErr.Status >= 400 && apiErr.Status < 500 {
				status = apiErr.Status
			}
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
