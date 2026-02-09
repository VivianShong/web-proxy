package main

import (
	"html/template"
	"log"
	"net/http"
	"strings"
)

type PageData struct {
	Blocked []string
	Logs    []RequestLog
}

// truncate is a helper function for the template
func truncate(s string, length int) string {
	if len(s) > length {
		return s[:length] + "..."
	}
	return s
}

func StartManagementServer(addr string, state *ProxyState) {
	tmpl := template.Must(template.New("dashboard.html").Funcs(template.FuncMap{
		"truncate": truncate,
	}).ParseFiles("dashboard.html"))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := PageData{
			Blocked: state.GetBlocked(),
			Logs:    state.GetLogs(),
		}
		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("Template error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			host := r.FormValue("host")
			host = strings.TrimSpace(host)
			if host != "" {
				// Sanitize host: remove scheme, path, port
				if strings.Contains(host, "://") {
					host = host[strings.Index(host, "://")+3:]
				}
				if idx := strings.Index(host, "/"); idx != -1 {
					host = host[:idx]
				}
				if idx := strings.Index(host, ":"); idx != -1 {
					host = host[:idx]
				}

				state.Block(host)
				// Basic persistence for now - saves on every change
				state.SaveBlocked("blocked.json")
			}
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	http.HandleFunc("/unblock", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			host := r.FormValue("host")
			if host != "" {
				state.Unblock(host)
				state.SaveBlocked("blocked.json")
			}
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	log.Printf("Management console listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
