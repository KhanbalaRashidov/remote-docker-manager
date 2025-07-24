package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/ssh"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type ServerConfig struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	State   string `json:"state"`
	Created string `json:"created"`
	Ports   string `json:"ports"`
}
type ContainerLog struct {
	Log string `json:"log"`
}
type DockerManager struct {
	config *ServerConfig
}

func (dm *DockerManager) executeSSHCommand(command string) (string, error) {
	config := &ssh.ClientConfig{
		User: dm.config.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(dm.config.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	address := dm.config.Host + ":" + dm.config.Port
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return "", fmt.Errorf("SSH connection to %s failed: %v", address, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("SSH session creation failed: %v", err)
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(command); err != nil {
		errorOutput := stderr.String()
		if errorOutput != "" {
			return "", fmt.Errorf("command '%s' failed: %v, stderr: %s", command, err, errorOutput)
		}
		return "", fmt.Errorf("command '%s' failed: %v", command, err)
	}

	output := stdout.String()
	fmt.Println("OUTPUT:", output)
	return output, nil
}
func (dm *DockerManager) LogContainers(containerId string) ([]ContainerLog, error) {
	_, err := dm.executeSSHCommand("which docker")
	if err != nil {
		return []ContainerLog{}, fmt.Errorf("Docker is not installed or not in PATH: %v", err)
	}
	formattedOutput, err := dm.executeSSHCommand(fmt.Sprintf("docker logs  --tail 20 %s 2>&1 | grep -v '^$'", containerId))
	if err != nil {
		return []ContainerLog{}, fmt.Errorf("Docker ps formatted command failed: %v", err)
	}

	lines := strings.Split(formattedOutput, "\n")
	var containers []ContainerLog

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := line
		if len(parts) >= 1 {
			container := ContainerLog{
				Log: parts,
			}

			containers = append(containers, container)
		}
	}

	return containers, nil
}

func (dm *DockerManager) GetContainers() ([]Container, error) {
	_, err := dm.executeSSHCommand("which docker")
	if err != nil {
		return []Container{}, fmt.Errorf("Docker is not installed or not in PATH: %v", err)
	}

	_, err = dm.executeSSHCommand("docker info")
	if err != nil {
		return []Container{}, fmt.Errorf("Docker daemon is not running or permission denied: %v", err)
	}

	output, err := dm.executeSSHCommand("docker ps -a")
	if err != nil {
		return []Container{}, fmt.Errorf("Docker ps command failed: %v", err)
	}

	if strings.TrimSpace(output) == "" {
		return []Container{}, fmt.Errorf("Docker ps returned empty output")
	}

	formattedOutput, err := dm.executeSSHCommand("docker ps -a --format '{{.ID}}|{{.Names}}|{{.Image}}|{{.Status}}|{{.CreatedAt}}|{{.Ports}}'")
	if err != nil {
		return []Container{}, fmt.Errorf("Docker ps formatted command failed: %v", err)
	}

	lines := strings.Split(formattedOutput, "\n")
	var containers []Container

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) >= 5 {
			container := Container{
				ID:      strings.TrimSpace(parts[0]),
				Name:    strings.TrimSpace(parts[1]),
				Image:   strings.TrimSpace(parts[2]),
				Status:  strings.TrimSpace(parts[3]),
				State:   getStateFromStatus(strings.TrimSpace(parts[3])),
				Created: strings.TrimSpace(parts[4]),
				Ports:   "",
			}
			if len(parts) > 5 {
				container.Ports = strings.TrimSpace(parts[5])
			}
			containers = append(containers, container)
		}
	}

	return containers, nil
}

func getStateFromStatus(status string) string {
	if strings.Contains(strings.ToLower(status), "up") {
		return "running"
	}
	return "stopped"
}

func (dm *DockerManager) StartContainer(containerID string) error {
	_, err := dm.executeSSHCommand(fmt.Sprintf("docker start %s", containerID))
	return err
}

func (dm *DockerManager) StopContainer(containerID string) error {
	_, err := dm.executeSSHCommand(fmt.Sprintf("docker stop %s", containerID))
	return err
}

func (dm *DockerManager) RestartContainer(containerID string) error {
	_, err := dm.executeSSHCommand(fmt.Sprintf("docker restart %s", containerID))
	return err
}

func (dm *DockerManager) RemoveContainer(containerID string) error {
	_, err := dm.executeSSHCommand(fmt.Sprintf("docker rm -f %s", containerID))
	return err
}

var dockerManager *DockerManager

const htmlTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Remote Docker Manager</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; background-color: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
        .server-info { background: #e3f2fd; padding: 10px; border-radius: 5px; margin-bottom: 20px; }
        table { width: 100%; border-collapse: collapse; margin-top: 20px; }
        th, td { border: 1px solid #ddd; padding: 12px; text-align: left; }
        th { background-color: #2196F3; color: white; }
        .running { color: #4CAF50; font-weight: bold; }
        .stopped { color: #f44336; font-weight: bold; }
        .btn { padding: 8px 16px; margin: 2px; border: none; border-radius: 4px; cursor: pointer; font-size: 12px; }
        .btn-primary { background: #2196F3; color: white; }
        .btn-success { background: #4CAF50; color: white; }
        .btn-warning { background: #FF9800; color: white; }
        .btn-danger { background: #f44336; color: white; }
        .btn:hover { opacity: 0.8; }
        .config-form { background: #f9f9f9; padding: 20px; border-radius: 5px; margin-bottom: 20px; }
        .form-group { margin-bottom: 15px; }
        .form-group label { display: block; margin-bottom: 5px; font-weight: bold; }
        .form-group input { width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 4px; }
        .loading { text-align: center; padding: 20px; }
        .error { color: #f44336; background: #ffebee; padding: 10px; border-radius: 4px; margin: 10px 0; }
        .success { color: #4CAF50; background: #e8f5e8; padding: 10px; border-radius: 4px; margin: 10px 0; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🐳 Remote Docker Manager</h1>
            <button class="btn btn-primary" onclick="showConfig()">Server Config</button>
        </div>

        <div id="configSection" class="config-form" style="display: none;">
            <h3>Server Configuration</h3>
            <div class="form-group">
                <label>Host:</label>
                <input type="text" id="host" placeholder="192.168.1.100" value="{{.Host}}">
            </div>
            <div class="form-group">
                <label>Port:</label>
                <input type="text" id="port" placeholder="22" value="{{.Port}}">
            </div>
            <div class="form-group">
                <label>Username:</label>
                <input type="text" id="username" placeholder="root" value="{{.Username}}">
            </div>
            <div class="form-group">
                <label>Password:</label>
                <input type="password" id="password" placeholder="password">
            </div>
            <button class="btn btn-success" onclick="saveConfig()">Connect & Save</button>
            <button class="btn btn-primary" onclick="hideConfig()">Cancel</button>
        </div>

        <div class="server-info">
            <strong>Connected Server:</strong> {{.Host}}:{{.Port}} ({{.Username}})
            <button class="btn btn-primary" onclick="refreshContainers()" style="float: right;">🔄 Refresh</button>
        </div>

        <div id="message"></div>
        <div id="loading" class="loading" style="display: none;">Loading containers...</div>
        
        <table id="containersTable">
            <thead>
                <tr>
                    <th>ID</th>
                    <th>Name</th>
                    <th>Image</th>
                    <th>Status</th>
                    <th>Created</th>
                    <th>Ports</th>
                    <th>Actions</th>
                </tr>
            </thead>
            <tbody id="containersBody">
            </tbody>
        </table>
    </div>

    <script>
        function showConfig() {
            document.getElementById('configSection').style.display = 'block';
        }

        function hideConfig() {
            document.getElementById('configSection').style.display = 'none';
        }

        function saveConfig() {
            const config = {
                host: document.getElementById('host').value,
                port: document.getElementById('port').value,
                username: document.getElementById('username').value,
                password: document.getElementById('password').value
            };

            fetch('/api/config', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(config)
            })
            .then(response => response.json())
            .then(data => {
                if (data.success) {
                    showMessage('Configuration saved successfully!', 'success');
                    hideConfig();
                    refreshContainers();
                } else {
                    showMessage('Error: ' + data.error, 'error');
                }
            })
            .catch(err => showMessage('Connection failed: ' + err, 'error'));
        }

        function refreshContainers() {
            document.getElementById('loading').style.display = 'block';
            document.getElementById('containersTable').style.display = 'none';

            fetch('/api/containers')
            .then(response => response.json())
            .then(data => {
                document.getElementById('loading').style.display = 'none';
                document.getElementById('containersTable').style.display = 'table';
                
                if (data.success) {
                    updateContainersTable(data.containers || []);
                } else {
                    showMessage('Error: ' + (data.error || 'Unknown error'), 'error');
                    updateContainersTable([]);
                }
            })
            .catch(err => {
                document.getElementById('loading').style.display = 'none';
                document.getElementById('containersTable').style.display = 'table';
                showMessage('Failed to fetch containers: ' + err.message, 'error');
                updateContainersTable([]);
            });
        }

        function updateContainersTable(containers) {
            const tbody = document.getElementById('containersBody');
            tbody.innerHTML = '';

            if (!containers || !Array.isArray(containers)) {
                tbody.innerHTML = '<tr><td colspan="7">No containers found</td></tr>';
                return;
            }

            containers.forEach(container => {
                const row = document.createElement('tr');
				row.id=container.id ;
                row.innerHTML = 
                    '<td>' + container.id + '</td>' +
                    '<td>' + container.name + '</td>' +
                    '<td>' + container.image + '</td>' +
                    '<td class="' + container.state + '">' + container.status + '</td>' +
                    '<td>' + container.created + '</td>' +
                    '<td>' + container.ports + '</td>' +
                    '<td>' +
                        '<button class="btn btn-success" onclick="containerAction(\'' + container.id + '\', \'start\')">▶️ Start</button>' +
                        '<button class="btn btn-warning" onclick="containerAction(\'' + container.id + '\', \'stop\')">⏸️ Stop</button>' +
                        '<button class="btn btn-primary" onclick="containerAction(\'' + container.id + '\', \'restart\')">🔄 Restart</button>' +
                        '<button class="btn btn-danger" onclick="containerAction(\'' + container.id + '\', \'remove\')">🗑️ Remove</button>' +
                        '<button class="btn btn-secondary" onclick="logAction(\'' + container.id + '\')">📝 Log</button>' +
                    '</td>';
                tbody.appendChild(row);
            });
        }

  		function logAction(containerID) {
			const existingRow = document.getElementById('log'+containerID);
			 if (existingRow) {
				existingRow.style.display = existingRow.style.display === 'none' ? 'table-row' : 'none';
			} else {
					fetch('/api/logs/' + containerID , {
						method: 'GET'
					})
					.then(response => response.json())
					.then(data => {
            		 updateLogTable(data.containers || [],containerID) 

					})
					.catch(err => showMessage('Action failed: ' + err, 'error'));
					
			}
        }

		function updateLogTable(containers,containerID) {
					const newRow = document.createElement('tr');
	                newRow.id='log'+containerID
					const td1 = document.createElement('td');
					td1.colSpan = 7; // O el número de columnas que tenga la tabla
   					// Crear lista y li
					const ul = document.createElement('ul');
					ul.style.fontSize = 'small';
				 containers.forEach(container => {
							console.log(container);
							const row = document.getElementById(containerID);
						
							const li = document.createElement('li');
							li.textContent = container.log; // o lo que quieras mostrar
							ul.appendChild(li);
							td1.appendChild(ul);
							newRow.appendChild(td1);
							row.parentNode.insertBefore(newRow, row.nextSibling);
						})
		}
        function containerAction(containerID, action) {
            if (action === 'remove' && !confirm('Are you sure you want to remove this container?')) {
                return;
            }

            fetch('/api/container/' + containerID + '/' + action, {
                method: 'POST'
            })
            .then(response => response.json())
            .then(data => {
                if (data.success) {
                    showMessage('Action completed successfully!', 'success');
                    refreshContainers();
                } else {
                    showMessage('Error: ' + data.error, 'error');
                }
            })
            .catch(err => showMessage('Action failed: ' + err, 'error'));
        }

        function showMessage(message, type) {
            const messageDiv = document.getElementById('message');
            messageDiv.innerHTML = '<div class="' + type + '">' + message + '</div>';
            setTimeout(() => messageDiv.innerHTML = '', 5000);
        }

        window.onload = function() {
            refreshContainers();
        };
    </script>
</body>
</html>
`

func homeHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.New("index").Parse(htmlTemplate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	config := &ServerConfig{}
	if dockerManager != nil && dockerManager.config != nil {
		config = dockerManager.config
	}

	tmpl.Execute(w, config)
}

func configHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var config ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		log.Printf("ERROR: Invalid JSON in config: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid JSON format",
		})
		return
	}

	if config.Host == "" || config.Username == "" || config.Password == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Host, username and password are required",
		})
		return
	}

	if config.Port == "" {
		config.Port = "22"
	}

	log.Printf("INFO: Attempting to connect to %s@%s:%s", config.Username, config.Host, config.Port)

	dockerManager = &DockerManager{config: &config}

	testOutput, err := dockerManager.executeSSHCommand("whoami && echo 'SSH connection successful'")
	if err != nil {
		log.Printf("ERROR: SSH test failed: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "SSH connection failed: " + err.Error(),
		})
		return
	}

	log.Printf("INFO: SSH test successful: %s", testOutput)

	// Docker mövcudluğunu test et
	dockerOutput, err := dockerManager.executeSSHCommand("docker --version")
	if err != nil {
		log.Printf("ERROR: Docker test failed: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Docker is not available: " + err.Error(),
		})
		return
	}

	log.Printf("INFO: Docker test successful: %s", dockerOutput)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Configuration saved successfully",
	})
}

func logsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	containerId := vars["id"]
	containers, err := dockerManager.LogContainers(containerId)
	if err != nil {
		log.Printf("ERROR: Failed to get containers: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    false,
			"error":      err.Error(),
			"containers": []Container{},
		})
		return
	}

	log.Printf("INFO: Successfully fetched %d containers", len(containers))
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"containers": containers,
		"count":      len(containers),
	})
}

func containersHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if dockerManager == nil {
		log.Println("ERROR: No docker manager configured")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    false,
			"error":      "No server configuration found. Please configure server first.",
			"containers": []Container{},
		})
		return
	}

	log.Printf("INFO: Fetching containers from %s@%s:%s",
		dockerManager.config.Username, dockerManager.config.Host, dockerManager.config.Port)

	containers, err := dockerManager.GetContainers()
	if err != nil {
		log.Printf("ERROR: Failed to get containers: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    false,
			"error":      err.Error(),
			"containers": []Container{},
		})
		return
	}

	log.Printf("INFO: Successfully fetched %d containers", len(containers))
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"containers": containers,
		"count":      len(containers),
	})
}

func containerActionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if dockerManager == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "No server configuration found",
		})
		return
	}

	vars := mux.Vars(r)
	containerID := vars["id"]
	action := vars["action"]

	var err error
	switch action {
	case "start":
		err = dockerManager.StartContainer(containerID)
	case "stop":
		err = dockerManager.StopContainer(containerID)
	case "restart":
		err = dockerManager.RestartContainer(containerID)
	case "remove":
		err = dockerManager.RemoveContainer(containerID)

	default:
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Unknown action: " + action,
		})
		return
	}

	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Action completed successfully",
	})
}

func main() {
	r := mux.NewRouter()

	r.HandleFunc("/", homeHandler)
	r.HandleFunc("/health", healthHandler)
	r.HandleFunc("/api/config", configHandler)
	r.HandleFunc("/api/containers", containersHandler)
	r.HandleFunc("/api/logs/{id}", logsHandler)
	r.HandleFunc("/api/container/{id}/{action}", containerActionHandler)

	r.Use(loggingMiddleware)

	port := ":8080"
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = ":" + envPort
	}

	fmt.Printf("🚀 Remote Docker Manager starting on http://localhost%s\n", port)
	fmt.Println("📋 Available endpoints:")
	fmt.Println("   GET  /           - Web interface")
	fmt.Println("   GET  /health     - Health check")
	fmt.Println("   POST /api/config - Server configuration")
	fmt.Println("   GET  /api/containers - List containers")
	fmt.Println("   POST /api/container/{id}/{action} - Container actions")
	fmt.Println("   POST /api/logs/{id} - Logs container")

	log.Fatal(http.ListenAndServe(port, r))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"version":   "1.0.0",
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.RequestURI, time.Since(start))
	})
}
