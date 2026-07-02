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
	"path/filepath"
	"time"

	"github.com/x-name15/replaydb/internal/domain"
	"github.com/x-name15/replaydb/internal/engine"
)

var templatesFS embed.FS

type DashboardData struct {
	DataDir      string
	LogSize      int64
	SnapshotSize int64
	SearchedKind string
	SearchedID   string
	StateJSON    string
	Error        string
}

func basicAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	user := os.Getenv("REDB_DASHBOARD_USER")
	pass := os.Getenv("REDB_DASHBOARD_PASS")

	if user == "" || pass == "" {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, ok := r.BasicAuth()

		// constant-time comparison to avoid leaking credential length/content via timing
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

func StartHTTPServer(port string, dataDir string, registry *domain.Registry, index *engine.Index) {
	tmpl, err := template.ParseFS(templatesFS, "templates/dashboard.html")
	if err != nil {
		log.Fatalf("server: failed to parse dashboard template: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", basicAuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		eventsPath := filepath.Join(dataDir, "events.redb")
		snapshotsPath := filepath.Join(dataDir, "snapshots.redb")

		var logSize, snapSize int64
		if fi, err := os.Stat(eventsPath); err == nil {
			logSize = fi.Size()
		}
		if fi, err := os.Stat(snapshotsPath); err == nil {
			snapSize = fi.Size()
		}

		data := DashboardData{
			DataDir:      dataDir,
			LogSize:      logSize,
			SnapshotSize: snapSize,
			SearchedKind: r.URL.Query().Get("kind"),
			SearchedID:   r.URL.Query().Get("id"),
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
	fmt.Printf("Web UI Monitor dashboard online at http://localhost%s%s\n", port, authNote)

	if err := srv.ListenAndServe(); err != nil {
		fmt.Printf("⚠ Failed to bind Web UI HTTP server: %v\n", err)
	}
}
