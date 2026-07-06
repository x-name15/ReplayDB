package server

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/x-name15/replaydb/internal/domain"
	"github.com/x-name15/replaydb/internal/engine"
	"github.com/x-name15/replaydb/internal/metrics"
)

//go:embed templates/dashboard.html
var templatesFS embed.FS

type DashboardData struct {
	DataDir        string
	Stats          metrics.Stats
	ArchiveEnabled bool
	LastCompaction *engine.CompactionInfo
	LastArchive    *engine.ArchiveCycleInfo
	SearchedKind   string
	SearchedID     string
	StateJSON      string
	Error          string
}

func basicAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	user := os.Getenv("REDB_DASHBOARD_USER")
	pass := os.Getenv("REDB_DASHBOARD_PASS")
	if user == "" || pass == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, ok := r.BasicAuth()
		userMatch := subtle.ConstantTimeCompare([]byte(gotUser), []byte(user)) == 1
		passMatch := subtle.ConstantTimeCompare([]byte(gotPass), []byte(pass)) == 1
		if !ok || !userMatch || !passMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="ReplayDB Dashboard"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func StartHTTPServer(port string, dataDir string, registry *domain.Registry, index *engine.Index, appender *engine.Appender, archiver *engine.Archiver) {
	tmpl, err := template.ParseFS(templatesFS, "templates/dashboard.html")
	if err != nil {
		log.Fatalf("server: failed to parse dashboard template: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", basicAuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		data := DashboardData{
			DataDir:        dataDir,
			Stats:          metrics.Snapshot(dataDir, index.Len),
			ArchiveEnabled: archiver != nil,
			SearchedKind:   r.URL.Query().Get("kind"),
			SearchedID:     r.URL.Query().Get("id"),
		}
		if appender != nil {
			data.LastCompaction = appender.LastCompaction()
		}
		if archiver != nil {
			data.LastArchive = archiver.LastCycle()
		}
		if data.SearchedID != "" && data.SearchedKind != "" {
			state, err := engine.ReplayStateAt(dataDir, data.SearchedKind, data.SearchedID, time.Now().UTC(), registry, index)
			if err != nil {
				data.Error = err.Error()
			} else {
				bytes, err := json.MarshalIndent(state, "", "  ")
				if err != nil {
					data.Error = fmt.Sprintf("failed to marshal state: %v", err)
				} else {
					data.StateJSON = string(bytes)
				}
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("server: template execute error: %v", err)
		}
	}))
	mux.HandleFunc("/metrics", metrics.Handler(dataDir, index.Len))
	srv := &http.Server{
		Addr:              port,
		Handler:           mux,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	authNote := ""
	if os.Getenv("REDB_DASHBOARD_USER") == "" || os.Getenv("REDB_DASHBOARD_PASS") == "" {
		authNote = " (⚠ no auth configured — set REDB_DASHBOARD_USER/REDB_DASHBOARD_PASS to lock it down)"
	}
	log.Printf("[boot] dashboard online at http://localhost%s%s\n", port, authNote)
	log.Printf("[boot] metrics available at http://localhost%s/metrics\n", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("⚠ Failed to bind Web UI HTTP server: %v\n", err)
	}
}
