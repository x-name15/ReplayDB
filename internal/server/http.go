package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/x-name15/replaydb/internal/engine"
)

// HTML Template embedded directly as a string to maintain single-binary zero-dependency goals
const dashboardTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>ReplayDB Operational Dashboard</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0f172a; color: #f8fafc; margin: 0; padding: 2rem; }
        .container { max-width: 1000px; margin: 0 auto; }
        h1 { color: #38bdf8; border-bottom: 2px solid #334155; padding-bottom: 0.5rem; }
        .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 1rem; margin-bottom: 2rem; }
        .card { background: #1e293b; padding: 1.5rem; border-radius: 8px; border: 1px solid #334155; }
        .card h3 { margin: 0 0 0.5rem 0; color: #94a3b8; font-size: 0.9rem; text-transform: uppercase; }
        .card p { margin: 0; font-size: 1.8rem; font-weight: bold; color: #f1f5f9; }
        .search-box { background: #1e293b; padding: 1.5rem; border-radius: 8px; border: 1px solid #38bdf8; margin-bottom: 2rem; display: flex; gap: 1rem; }
        input { flex: 1; padding: 0.75rem; border-radius: 4px; border: 1px solid #475569; background: #0f172a; color: white; font-size: 1rem; }
        button { background: #38bdf8; color: #0f172a; font-weight: bold; padding: 0.75rem 1.5rem; border: none; border-radius: 4px; cursor: pointer; font-size: 1rem; }
        button:hover { background: #0ea5e9; }
        pre { background: #0f172a; padding: 1rem; border-radius: 6px; border: 1px solid #334155; overflow-x: auto; color: #34d399; font-size: 0.95rem; }
        .footer { text-align: center; margin-top: 3rem; color: #475569; font-size: 0.85rem; }
    </style>
</head>
<body>
    <div class="container">
        <h1>📊 ReplayDB Live Core Monitor</h1>
        
        <div class="stats-grid">
            <div class="card">
                <h3>Data Directory</h3>
                <p style="font-size: 1.2rem; word-break: break-all;">{{.DataDir}}</p>
            </div>
            <div class="card">
                <h3>Log Payload Size</h3>
                <p>{{.LogSize}} Bytes</p>
            </div>
            <div class="card">
                <h3>Snapshot Store Size</h3>
                <p>{{.SnapshotSize}} Bytes</p>
            </div>
        </div>

        <div class="card">
            <h2>🔍 Dynamic State Inspector</h2>
            <form method="GET" class="search-box">
                <input type="text" name="id" placeholder="Enter Aggregate ID (e.g., test-order-99)" value="{{.SearchedID}}" required>
                <button type="submit">Reconstruct State</button>
            </form>

            {{if .SearchedID}}
                <h3>Reconstructed Materialized View (Time-Travel: NOW)</h3>
                {{if .Error}}
                    <p style="color: #ef4444;">❌ Error: {{.Error}}</p>
                {{else}}
                    <pre>{{.StateJSON}}</pre>
                {{end}}
            {{else}}
                <p style="color: #64748b;">Submit an Aggregate ID above to stream its binary logs and view its state layout.</p>
            {{end}}
        </div>

        <div class="footer">ReplayDB Engine v1.0.0 • Architecture Phase 6 Execution</div>
    </div>
</body>
</html>
`

type DashboardData struct {
	DataDir      string
	LogSize      int64
	SnapshotSize int64
	SearchedID   string
	StateJSON    string
	Error        string
}

// StartHTTPServer fires up the monitoring panel asynchronously
func StartHTTPServer(port string, dataDir string) {
	tmpl := template.Must(template.New("dashboard").Parse(dashboardTemplate))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
			SearchedID:   r.URL.Query().Get("id"),
		}

		if data.SearchedID != "" {
			// Trigger an internal fast-forward replay directly into our core memory domain
			state, err := engine.ReplayStateAt(dataDir, data.SearchedID, time.Now().UTC())
			if err != nil {
				data.Error = err.Error()
			} else {
				bytes, _ := json.MarshalIndent(state, "", "  ")
				data.StateJSON = string(bytes)
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, data)
	})

	fmt.Printf("🌐 Web UI Monitor dashboard online at http://localhost%s\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("⚠️ Failed to bind Web UI HTTP server: %v\n", err)
	}
}