// Get token from URL or use provided token
let token = "";
const urlParams = new URLSearchParams(window.location.search);
if (urlParams.has('token')) {
    token = urlParams.get('token');
    // Store token in sessionStorage
    sessionStorage.setItem('auth-token', token);
    // Remove token from URL without page reload
    window.history.replaceState({}, document.title, window.location.pathname);
} else {
    // Try to get token from sessionStorage
    const storedToken = sessionStorage.getItem('auth-token');
    if (storedToken) {
        token = storedToken;
    }
}

// Create hidden input with token for WebSocket connection
const tokenInput = document.createElement('input');
tokenInput.type = 'hidden';
tokenInput.id = 'auth-token';
tokenInput.value = token;
document.body.appendChild(tokenInput);

// Connection retry logic
let reconnectAttempts = 0;
const maxReconnectAttempts = 10;
const baseRetryDelay = 1000; // 1 second

let ws;
let reconnectTimeout;

function connect() {
    // Clear any previous connection
    if (ws) {
        ws.onopen = null;
        ws.onmessage = null;
        ws.onerror = null;
        ws.onclose = null;
        try {
            ws.close();
        } catch (e) {}
    }

    // Create new WebSocket connection
    ws = new WebSocket("ws://" + location.host + "/ws?token=" + token);

    ws.onopen = function() {
        reconnectAttempts = 0; // Reset attempts on successful connection
        addMessage("System", "Connected to server", "info");
    };

    ws.onmessage = function(event) {
        const msg = JSON.parse(event.data);
        handleMessage(msg);
    };

    ws.onerror = function(error) {
        addMessage("Error", "Connection error", "danger");
    };

    ws.onclose = function() {
        // Only attempt to reconnect if we had a successful connection before
        // or if this is the first few attempts (to handle startup)
        if (reconnectAttempts < maxReconnectAttempts) {
            reconnectAttempts++;
            const retryDelay = baseRetryDelay * Math.pow(2, reconnectAttempts - 1); // Exponential backoff
            addMessage("System", `Attempting to reconnect in ${retryDelay/1000} seconds... (${reconnectAttempts}/${maxReconnectAttempts})`, "warning");

            // Clear any existing timeout
            if (reconnectTimeout) {
                clearTimeout(reconnectTimeout);
            }

            // Set up retry
            reconnectTimeout = setTimeout(() => {
                connect();
            }, retryDelay);
        } else {
            addMessage("Error", "Max reconnection attempts reached. Please refresh the page.", "danger");
        }
    }

    // Create references to DOM elements
    const chatForm = document.getElementById('chat-form');
    const messageInput = document.getElementById('message-input');
    const messages = document.getElementById('messages');
    const clearBtn = document.getElementById('clear-btn');

    // Initialize connection
    connect();

    // Set up form submission handler
    chatForm.onsubmit = function(e) {
        e.preventDefault();
        const message = messageInput.value.trim();
        if (message) {
            ws.send(JSON.stringify({
                type: "chat",
                role: "user",
                content: message
            }));
            messageInput.value = "";
        }
    };

    clearBtn.onclick = function() {
        ws.send(JSON.stringify({ type: "clear" }));
        messages.innerHTML = "";
    };

    function handleMessage(msg) {
        switch(msg.type) {
            case "chat":
                addMessage(msg.role, msg.content, msg.role === "user" ? "primary" : "secondary");
                break;
            case "tool_interaction":
                addToolInteraction(msg.tool_name, msg.tool_id, msg.description, msg.status, msg.result, msg.error);
                break;
            case "tool_call":
                // Legacy format - convert to tool_interaction
                addToolInteraction(msg.tool_name, msg.tool_id, msg.description, "calling", "", "");
                break;
            case "tool_result":
                addToolResult(msg.tool_id, msg.result, msg.error);
                break;
            case "error":
                addMessage("Error", msg.content, "danger");
                break;
            case "system":
                addMessage("System", msg.content, "info");
                break;
        }
    }

    function addMessage(role, content, variant) {
        const div = document.createElement("div");
        div.className = "alert alert-" + variant + " mb-2";
        div.innerHTML = "<strong>" + escapeHtml(role) + ":</strong> " + escapeHtml(content);
        messages.appendChild(div);
        messages.scrollTop = messages.scrollHeight;
    }

    function addToolCall(name, id) {
        const div = document.createElement("div");
        div.className = "alert alert-secondary mb-2";
        div.innerHTML = "<strong>Tool Call:</strong> " + escapeHtml(name) + " <small class='text-muted'>(ID: " + escapeHtml(id) + ")</small>";
        div.id = "tool-" + id;
        messages.appendChild(div);
        messages.scrollTop = messages.scrollHeight;
    }

    function addToolInteraction(name, id, description, status, result, error) {
        // Find existing tool div or create new one
        let div = document.getElementById("tool-" + id);
        
        if (!div && status === "calling") {
            // Create new tool interaction display
            div = document.createElement("div");
            div.className = "alert alert-secondary mb-2";
            div.id = "tool-" + id;
            
            let html = "<strong>Tool Call:</strong> " + escapeHtml(name);
            if (description) {
                html += " <em class='text-muted'>(" + escapeHtml(description) + ")</em>";
            }
            html += " <small class='text-muted'>(ID: " + escapeHtml(id) + ")</small>";
            div.innerHTML = html;
            messages.appendChild(div);
            messages.scrollTop = messages.scrollHeight;
        } else if (div && (status === "completed" || status === "error")) {
            // Update existing tool with result
            if (error) {
                div.className = "alert alert-danger mb-2";
                div.innerHTML += "<br><strong>Error:</strong> " + escapeHtml(error);
            } else {
                div.className = "alert alert-success mb-2";
                div.innerHTML += " <span class='badge bg-success'>Completed</span>";
            }
        }
    }

    function addToolResult(id, result, error) {
        const div = document.createElement("div");
        if (error) {
            div.className = "alert alert-danger mb-2";
            div.innerHTML = "<strong>Tool Error:</strong> " + escapeHtml(error);
        } else {
            div.className = "alert alert-light mb-2 border";
            div.innerHTML = "<strong>Tool Result:</strong> <pre class='mb-0'>" + escapeHtml(String(result)) + "</pre>";
        }
        messages.appendChild(div);
        messages.scrollTop = messages.scrollHeight;
    }

    function escapeHtml(text) {
        const map = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' };
        return text.replace(/[&<>'"]/g, function(m) { return map[m]; });
    }