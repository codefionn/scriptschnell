// Comprehensive token handling with multiple fallback strategies
function getToken() {
    // Priority order for token retrieval:
    // 1. Window variable (set by server)
    if (window.authToken) {
        console.log('Token found in window.authToken');
        return window.authToken;
    }

    // 2. URL query parameter
    try {
        const url = new URL(window.location.href);
        const urlToken = url.searchParams.get('token');
        if (urlToken) {
            console.log('Token found in URL query parameter');
            
            // Clean up URL by removing token parameter
            if (window.history.pushState !== undefined) {
                const searchParams = new URLSearchParams(url.search);
                searchParams.delete('token');
                const newUrl = new URL(url);
                newUrl.search = searchParams.toString();
                window.history.pushState(undefined, "", newUrl.href);
                
                // Add popstate listener for cleanup
                window.addEventListener('popstate', function(event) {
                    // Handle browser back/forward navigation
                    console.log('Popstate event triggered');
                });
            }
            
            return urlToken;
        }
    } catch (error) {
        console.error('Error parsing token from URL:', error);
    }

    // 3. URL hash fragment (fallback)
    try {
        const hash = window.location.hash.substring(1); // Remove #
        if (hash && hash.includes('token=')) {
            const params = new URLSearchParams(hash);
            const hashToken = params.get('token');
            if (hashToken) {
                console.log('Token found in URL hash fragment');
                // Clean up hash
                window.location.hash = '';
                return hashToken;
            }
        }
    } catch (error) {
        console.error('Error parsing token from hash:', error);
    }

    // 4. sessionStorage fallback
    try {
        const storedToken = sessionStorage.getItem('auth-token');
        if (storedToken) {
            console.log('Token found in sessionStorage');
            return storedToken;
        }
    } catch (error) {
        console.error('Error accessing sessionStorage:', error);
    }

    // 5. Hidden input field (if exists)
    const hiddenInput = document.getElementById('auth-token');
    if (hiddenInput && hiddenInput.value) {
        console.log('Token found in hidden input field');
        return hiddenInput.value;
    }

    console.warn('No token found in any source');
    return null;
}

// Get and validate token
let token = getToken();

// Validate token format (basic validation)
function isValidToken(token) {
    if (!token) return false;
    if (typeof token !== 'string') return false;
    if (token.length < 10) return false; // Basic length check
    if (token.includes(' ')) return false; // No spaces allowed
    return true;
}

// Store token in sessionStorage if valid
if (token && isValidToken(token)) {
    try {
        sessionStorage.setItem('auth-token', token);
        console.log('Token stored in sessionStorage');
    } catch (error) {
        console.error('Failed to store token in sessionStorage:', error);
    }
} else if (token) {
    console.warn('Token validation failed, not storing in sessionStorage');
    token = null;
}

// Create or update hidden input field with token
function updateTokenInput() {
    let tokenInput = document.getElementById('auth-token');
    if (!tokenInput) {
        tokenInput = document.createElement('input');
        tokenInput.type = 'hidden';
        tokenInput.id = 'auth-token';
        document.body.appendChild(tokenInput);
    }
    tokenInput.value = token || '';
}

updateTokenInput();

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
        } catch (e) {
            console.error('Error closing WebSocket:', e);
        }
    }

    if (!token) {
        addMessage("Error", "No authentication token available. Please refresh the page.", "danger");
        return;
    }

    // Create WebSocket connection with token in query parameter
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = new URL(`${protocol}//${window.location.host}/ws`);
    wsUrl.searchParams.set('token', token);

    console.log('Attempting WebSocket connection to:', wsUrl.toString().replace(token, '[TOKEN]'));

    try {
        ws = new WebSocket(wsUrl.toString());
    } catch (error) {
        console.error('Failed to create WebSocket:', error);
        addMessage("Error", "Failed to establish WebSocket connection", "danger");
        scheduleReconnect();
        return;
    }

    ws.onopen = function() {
        reconnectAttempts = 0; // Reset attempts on successful connection
        console.log('WebSocket connection established');
        addMessage("System", "Connected to server", "info");
    };

    ws.onmessage = function(event) {
        try {
            const msg = JSON.parse(event.data);
            handleMessage(msg);
        } catch (error) {
            console.error('Error parsing message:', error);
        }
    };

    ws.onerror = function(error) {
        console.error('WebSocket error:', error);
        addMessage("Error", "Connection error", "danger");
    };

    ws.onclose = function(event) {
        console.log('WebSocket connection closed:', event.code, event.reason);
        scheduleReconnect();
    };
}

function scheduleReconnect() {
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
            console.log('Attempting reconnection...');
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
if (token) {
    connect();
} else {
    addMessage("Error", "No authentication token found. Please refresh the page.", "danger");
}

// Set up form submission handler
if (chatForm) {
    chatForm.onsubmit = function(e) {
        e.preventDefault();
        const message = messageInput.value.trim();
        if (message) {
            // Check if WebSocket connection is ready
            if (ws && ws.readyState === WebSocket.OPEN) {
                try {
                    ws.send(JSON.stringify({
                        type: "chat",
                        role: "user",
                        content: message
                    }));
                    messageInput.value = "";
                } catch (error) {
                    console.error('Error sending message:', error);
                    addMessage("Error", "Failed to send message", "danger");
                }
            } else {
                addMessage("System", "Connection not ready. Please wait for connection to establish.", "warning");
            }
        }
    };
}

if (clearBtn) {
    clearBtn.onclick = function() {
        // Send clear message to server
        if (ws && ws.readyState === WebSocket.OPEN) {
            try {
                ws.send(JSON.stringify({ type: "clear" }));
                // Clear the UI immediately for better user experience
                messages.innerHTML = "";
                addMessage("System", "Clearing session...", "info");
            } catch (error) {
                console.error('Error clearing session:', error);
            }
        } else {
            addMessage("Error", "Connection not ready. Cannot clear session.", "danger");
        }
    };
}

function handleMessage(msg) {
    switch(msg.type) {
        case "chat":
            addMessage(msg.role, msg.content, msg.role === "user" ? "primary" : "secondary");
            break;
        case "tool_call":
            addToolCall(msg.tool_name, msg.tool_id);
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
    if (!messages) return;
    
    const div = document.createElement("div");
    div.className = "alert alert-" + variant + " mb-2";
    div.innerHTML = "<strong>" + escapeHtml(role) + ":</strong> " + escapeHtml(content);
    messages.appendChild(div);
    messages.scrollTop = messages.scrollHeight;
}

function addToolCall(name, id) {
    if (!messages) return;
    
    const div = document.createElement("div");
    div.className = "alert alert-secondary mb-2";
    div.innerHTML = "<strong>Tool Call:</strong> " + escapeHtml(name) + " <small class='text-muted'>(ID: " + escapeHtml(id) + ")</small>";
    messages.appendChild(div);
    messages.scrollTop = messages.scrollHeight;
}

function addToolResult(id, result, error) {
    if (!messages) return;
    
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
    if (text === null || text === undefined) return '';
    const map = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' };
    return String(text).replace(/[&<>"']/g, function(m) { return map[m]; });
}

// Handle visibility change to reconnect when page becomes visible again
document.addEventListener('visibilitychange', function() {
    if (!document.hidden && ws && ws.readyState !== WebSocket.OPEN) {
        console.log('Page became visible, attempting to reconnect...');
        connect();
    }
});

// Handle page unload to close connection cleanly
window.addEventListener('beforeunload', function() {
    if (ws) {
        try {
            ws.close();
        } catch (e) {
            console.error('Error closing WebSocket on unload:', e);
        }
    }
});