// Comprehensive token handling with multiple fallback strategies
function getToken() {
    // Priority order for token retrieval:
    // 1. Window variable (set by server)
    if (window.authToken) {
        console.log('Token found in window.authToken');
        return window.authToken;
    }

    // 2. URL query parameter (fallback if window.authToken is not set)
    try {
        const url = new URL(window.location.href);
        const urlToken = url.searchParams.get('token');
        if (urlToken) {
            console.log('Token found in URL query parameter');
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

// Track active tool interactions
const activeToolInteractions = new Map();

// Track if a tool interaction is the last message
function isLastMessage(element) {
    const messages = document.getElementById('messages');
    if (!messages) return false;
    const lastChild = messages.lastElementChild;
    return lastChild === element;
}

// Toggle tool section visibility
function toggleToolSection(toolId, section) {
    const content = document.getElementById(`tool-${toolId}-${section}`);
    const icon = document.getElementById(`tool-${toolId}-${section}-icon`);
    if (content && icon) {
        if (content.style.display === 'none') {
            content.style.display = 'block';
            icon.classList.remove('bi-chevron-right');
            icon.classList.add('bi-chevron-down');
        } else {
            content.style.display = 'none';
            icon.classList.remove('bi-chevron-down');
            icon.classList.add('bi-chevron-right');
        }
    }
}

// Format parameters for display
function formatParameters(params) {
    if (!params || Object.keys(params).length === 0) return 'No parameters';
    try {
        return JSON.stringify(params, null, 2);
    } catch (e) {
        return String(params);
    }
}

// Format result for display (truncate if too long)
function formatResult(result, maxLength = 300) {
    let resultStr;
    if (typeof result === 'object') {
        try {
            resultStr = JSON.stringify(result, null, 2);
        } catch (e) {
            resultStr = String(result);
        }
    } else {
        resultStr = String(result || '');
    }
    if (resultStr.length > maxLength) {
        return resultStr.substring(0, maxLength) + '\n... (truncated)';
    }
    return resultStr;
}

// Status bar management
function updateStatusBar(status, text) {
    const statusBar = document.getElementById('status-bar');
    const statusText = document.getElementById('status-text');
    const statusIndicator = statusBar.querySelector('.status-indicator');
    
    if (statusBar && statusText && statusIndicator) {
        // Update status bar class
        statusBar.className = 'status-bar ' + status + ' d-flex align-items-center';
        // Update indicator class
        statusIndicator.className = 'status-indicator ' + status;
        // Update text
        statusText.textContent = text;
    }
}

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
        updateStatusBar('connected', 'Connected');
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
        updateStatusBar('error', 'Connection error');
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
        
        updateStatusBar('connecting', `Reconnecting in ${retryDelay/1000}s (${reconnectAttempts}/${maxReconnectAttempts})`);

        // Clear any existing timeout
        if (reconnectTimeout) {
            clearTimeout(reconnectTimeout);
        }

        // Set up retry
        reconnectTimeout = setTimeout(() => {
            console.log('Attempting reconnection...');
            updateStatusBar('connecting', 'Connecting...');
            connect();
        }, retryDelay);
    } else {
        updateStatusBar('error', 'Connection failed - refresh page');
    }
}

// Create references to DOM elements
const chatForm = document.getElementById('chat-form');
const messageInput = document.getElementById('message-input');
const messages = document.getElementById('messages');
const sendBtn = document.getElementById('send-btn');
const stopBtn = document.getElementById('stop-btn');
const clearBtn = document.getElementById('clear-btn');

// Track whether the LLM is currently generating
let isProcessing = false;

function setProcessing(processing) {
    isProcessing = processing;
    if (processing) {
        sendBtn.classList.add('d-none');
        stopBtn.classList.remove('d-none');
        messageInput.disabled = true;
    } else {
        stopBtn.classList.add('d-none');
        sendBtn.classList.remove('d-none');
        messageInput.disabled = false;
        messageInput.focus();
    }
}

// Initialize connection
if (token) {
    updateStatusBar('connecting', 'Connecting...');
    connect();
} else {
    updateStatusBar('error', 'No auth token - refresh page');
}

// Set up form submission handler
if (chatForm) {
    chatForm.onsubmit = function(e) {
        e.preventDefault();
        const message = messageInput.value.trim();
        if (message && !isProcessing) {
            // Check if WebSocket connection is ready
            if (ws && ws.readyState === WebSocket.OPEN) {
                try {
                    ws.send(JSON.stringify({
                        type: "chat",
                        role: "user",
                        content: message
                    }));
                    messageInput.value = "";
                    setProcessing(true);
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

// Set up stop button handler
if (stopBtn) {
    stopBtn.onclick = function() {
        if (ws && ws.readyState === WebSocket.OPEN) {
            try {
                ws.send(JSON.stringify({ type: "stop" }));
            } catch (error) {
                console.error('Error sending stop message:', error);
            }
        }
        setProcessing(false);
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
            if (msg.role === "assistant") {
                addMessage(msg.role, msg.content, "secondary", true); // Enable markdown for assistant
                setProcessing(false);
            } else {
                addMessage(msg.role, msg.content, msg.role === "user" ? "primary" : "secondary");
            }
            break;
        case "tool_interaction":
            handleToolInteraction(msg);
            break;
        case "tool_call":
            // Legacy tool call - convert to compact interaction
            handleToolInteraction({
                type: "tool_interaction",
                tool_name: msg.tool_name,
                tool_id: msg.tool_id,
                status: "calling",
                compact: false
            });
            break;
        case "tool_result":
            // Legacy tool result - update existing interaction
            updateToolResult(msg.tool_id, msg.result, msg.error);
            break;
        case "authorization_request":
            handleAuthorizationRequest(msg);
            break;
        case "question_request":
            handleQuestionRequest(msg);
            break;
        case "error":
            addMessage("Error", msg.content, "danger");
            setProcessing(false);
            break;
        case "system":
            // Filter out connection-related system messages (they're shown in status bar)
            if (msg.content && 
                (msg.content.includes("Connection") || 
                 msg.content.includes("connect") || 
                 msg.content.includes("Reconnect") ||
                 msg.content.includes("reconnect"))) {
                // Update status bar instead of showing in chat
                if (msg.content.includes("Connected")) {
                    updateStatusBar('connected', 'Connected');
                } else if (msg.content.includes("error") || msg.content.includes("Error")) {
                    updateStatusBar('error', msg.content);
                }
                return;
            }
            // Show other system messages in chat
            if (msg.content) {
                addMessage("System", msg.content, "info");
                // Special handling for session cleared message
                if (msg.content === "Session cleared") {
                    // Remove the "Clearing session..." message and show confirmation
                    const tempMessages = messages.querySelectorAll('.alert-info');
                    tempMessages.forEach(el => {
                        if (el.textContent.includes("Clearing session...")) {
                            el.remove();
                        }
                    });
                    addMessage("System", "Session cleared successfully", "success");
                }
                // Reset processing state when generation is stopped
                if (msg.content === "Generation stopped") {
                    setProcessing(false);
                }
            }
            break;
    }
}

function handleToolInteraction(msg) {
    const toolId = msg.tool_id;
    
    if (msg.status === "calling") {
        // Create new compact tool interaction
        const div = document.createElement("div");
        div.className = "tool-card tool-interaction";
        div.id = `tool-${toolId}`;
        div.dataset.toolName = msg.tool_name;
        div.dataset.parameters = JSON.stringify(msg.parameters || {});
        
        // Build the compact tool card HTML
        const paramsStr = formatParameters(msg.parameters);
        
        div.innerHTML = `
            <div class="tool-header" onclick="toggleToolSection('${toolId}', 'body')">
                <i class="bi bi-tools text-secondary"></i>
                <span class="tool-name">${escapeHtml(msg.tool_name)}</span>
                <span class="tool-status text-muted">
                    <span class="spinner-border spinner-border-sm" role="status" aria-hidden="true"></span>
                    Running...
                </span>
            </div>
            <div class="tool-body" id="tool-${toolId}-body" style="display: block;">
                <div class="tool-section">
                    <div class="tool-section-header" onclick="event.stopPropagation(); toggleToolSection('${toolId}', 'input')">
                        <i class="bi bi-chevron-down" id="tool-${toolId}-input-icon"></i>
                        Input
                    </div>
                    <div class="tool-section-content" id="tool-${toolId}-input">${escapeHtml(paramsStr)}</div>
                </div>
                <div class="tool-section" id="tool-${toolId}-output-section" style="display: none;">
                    <div class="tool-section-header">
                        <i class="bi bi-chevron-right"></i>
                        Output
                    </div>
                    <div class="tool-section-content" id="tool-${toolId}-output"></div>
                </div>
            </div>
        `;
        
        messages.appendChild(div);
        messages.scrollTop = messages.scrollHeight;
        
        // Store reference for updates
        activeToolInteractions.set(toolId, div);
    } else if (msg.status === "completed" || msg.status === "error") {
        // Update existing interaction
        updateToolResult(toolId, msg.result, msg.error);
    }
}

function updateToolResult(toolId, result, error) {
    const div = activeToolInteractions.get(toolId);
    if (!div) return;
    
    const outputSection = document.getElementById(`tool-${toolId}-output-section`);
    const outputContent = document.getElementById(`tool-${toolId}-output`);
    const statusSpan = div.querySelector('.tool-status');
    const headerIcon = div.querySelector('.tool-header i:first-child');
    
    // Collapse input by default when result arrives (unless it's the last message)
    const inputContent = document.getElementById(`tool-${toolId}-input`);
    const inputIcon = document.getElementById(`tool-${toolId}-input-icon`);
    if (inputContent && inputIcon && !isLastMessage(div)) {
        inputContent.style.display = 'none';
        inputIcon.classList.remove('bi-chevron-down');
        inputIcon.classList.add('bi-chevron-right');
    }
    
    if (error) {
        div.classList.add('border-danger');
        if (headerIcon) {
            headerIcon.classList.remove('text-secondary');
            headerIcon.classList.add('text-danger');
        }
        if (statusSpan) {
            statusSpan.innerHTML = '<i class="bi bi-x-circle text-danger"></i> Failed';
            statusSpan.classList.add('text-danger');
        }
        if (outputContent) {
            outputContent.textContent = error;
            outputContent.style.color = '#dc3545';
        }
        if (outputSection) {
            outputSection.style.display = 'block';
        }
    } else {
        div.classList.add('border-success');
        if (headerIcon) {
            headerIcon.classList.remove('text-secondary');
            headerIcon.classList.add('text-success');
        }
        if (statusSpan) {
            statusSpan.innerHTML = '<i class="bi bi-check-circle text-success"></i> Completed';
            statusSpan.classList.add('text-success');
        }
        if (outputContent) {
            outputContent.textContent = formatResult(result);
        }
        if (outputSection) {
            outputSection.style.display = 'block';
        }
    }
    
    // Remove from active interactions
    activeToolInteractions.delete(toolId);
    messages.scrollTop = messages.scrollHeight;
}

function addMessage(role, content, variant, isMarkdown = false) {
    if (!messages) return;
    
    const div = document.createElement("div");
    div.className = "alert alert-" + variant + " mb-1 message-content py-2";
    
    if (isMarkdown && content) {
        // For assistant messages, format as markdown
        div.innerHTML = `
            <div class="d-flex justify-content-between align-items-start">
                <strong class="small">${escapeHtml(role)}:</strong>
                <small class="text-muted timestamp">${new Date().toLocaleTimeString()}</small>
            </div>
            <div class="markdown-content mt-1">${formatMarkdown(content)}</div>
        `;
    } else {
        // For other messages, plain text but preserve line breaks
        const formattedContent = content ? escapeHtml(content).replace(/\n/g, '<br>') : '';
        div.innerHTML = `
            <div class="d-flex justify-content-between align-items-start">
                <strong class="small">${escapeHtml(role)}:</strong>
                <small class="text-muted timestamp">${new Date().toLocaleTimeString()}</small>
            </div>
            <div class="mt-1 small">${formattedContent}</div>
        `;
    }
    
    messages.appendChild(div);
    messages.scrollTop = messages.scrollHeight;
}

function formatMarkdown(text) {
    if (!text) return '';
    
    // Escape HTML first
    let formatted = escapeHtml(text);
    
    // Headers
    formatted = formatted.replace(/^### (.+)$/gm, '<h6 class="mt-2 mb-1">$1</h6>');
    formatted = formatted.replace(/^## (.+)$/gm, '<h5 class="mt-2 mb-1">$1</h5>');
    formatted = formatted.replace(/^# (.+)$/gm, '<h4 class="mt-2 mb-1">$1</h4>');
    
    // Bold - fix the regex to handle **text**
    formatted = formatted.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    
    // Italic - fix the regex to handle *text*
    formatted = formatted.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    
    // Code blocks
    formatted = formatted.replace(/```([\s\S]*?)```/g, '<pre><code>$1</code></pre>');
    
    // Inline code
    formatted = formatted.replace(/`([^`]+)`/g, '<code>$1</code>');
    
    // Lists - handle both * and - for bullets
    formatted = formatted.replace(/^\* (.+)$/gm, '<li>$1</li>');
    formatted = formatted.replace(/^- (.+)$/gm, '<li>$1</li>');
    
    // Wrap consecutive list items in ul
    const listRegex = /(<li>.*<\/li>\s*)+/g;
    formatted = formatted.replace(listRegex, '<ul class="mb-2">$&</ul>');
    
    // Line breaks - convert single newlines to <br> for better formatting
    // but preserve double newlines as paragraph breaks
    formatted = formatted.replace(/\n\n/g, '</p><p>');
    formatted = formatted.replace(/\n/g, '<br>');
    
    // Wrap in paragraph if not already wrapped
    if (!formatted.startsWith('<') && formatted.trim() !== '') {
        formatted = '<p>' + formatted + '</p>';
    }
    
    return formatted;
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

// ==================== Menu and Modal Functions ====================

// Helper function to send WebSocket message
function sendMessage(type, data = {}) {
    if (ws && ws.readyState === WebSocket.OPEN) {
        const msg = { type, ...data };
        ws.send(JSON.stringify(msg));
    }
}

// Initialize modals when Bootstrap is ready
let settingsModal, providersModal, editProviderModal, modelsModal, searchModal, passwordModal, mcpModal;

document.addEventListener('DOMContentLoaded', function() {
    // Initialize Bootstrap modals
    settingsModal = new bootstrap.Modal(document.getElementById('settingsModal'));
    providersModal = new bootstrap.Modal(document.getElementById('providersModal'));
    editProviderModal = new bootstrap.Modal(document.getElementById('editProviderModal'));
    modelsModal = new bootstrap.Modal(document.getElementById('modelsModal'));
    searchModal = new bootstrap.Modal(document.getElementById('searchModal'));
    passwordModal = new bootstrap.Modal(document.getElementById('passwordModal'));
    mcpModal = new bootstrap.Modal(document.getElementById('mcpModal'));

    // Set up modal event listeners
    document.getElementById('settingsModal').addEventListener('shown.bs.modal', () => {});
    document.getElementById('providersModal').addEventListener('shown.bs.modal', loadProviders);
    document.getElementById('modelsModal').addEventListener('shown.bs.modal', loadModels);
    document.getElementById('searchModal').addEventListener('shown.bs.modal', loadSearchConfig);
    document.getElementById('mcpModal').addEventListener('shown.bs.modal', loadMCPServers);

    // Set up form handlers
    setupProviderForm();
    setupModelsForm();
    setupSearchForm();
    setupPasswordForm();
    setupMCPForm();
});

// ==================== Provider Management ====================

function loadProviders() {
    sendMessage('get_providers');
}

function showAddProvider(providerType) {
    const form = document.getElementById('add-provider-form');
    const typeInput = document.getElementById('providerType');
    const typeDisplay = document.getElementById('providerTypeDisplay');
    const baseUrlField = document.getElementById('baseUrlField');
    const apiKeyInput = document.getElementById('providerApiKey');
    const baseUrlInput = document.getElementById('providerBaseUrl');

    typeInput.value = providerType;
    typeDisplay.value = providerType.charAt(0).toUpperCase() + providerType.slice(1);
    
    // Show base URL field for openai-compatible providers
    if (providerType === 'openai-compatible') {
        baseUrlField.style.display = 'block';
    } else {
        baseUrlField.style.display = 'none';
    }

    // Clear inputs
    apiKeyInput.value = '';
    baseUrlInput.value = '';

    form.style.display = 'block';
}

function hideAddProvider() {
    document.getElementById('add-provider-form').style.display = 'none';
}

function setupProviderForm() {
    document.getElementById('providerForm').addEventListener('submit', function(e) {
        e.preventDefault();
        
        const providerType = document.getElementById('providerType').value;
        const apiKey = document.getElementById('providerApiKey').value;
        const data = {
            provider_type: providerType,
            api_key: apiKey
        };

        if (providerType === 'openai-compatible') {
            data.base_url = document.getElementById('providerBaseUrl').value;
        }

        sendMessage('add_provider', { data });
    });

    // Setup edit provider form
    document.getElementById('editProviderForm').addEventListener('submit', function(e) {
        e.preventDefault();
        saveProviderChanges();
    });

    // Setup save button in modal footer
    document.getElementById('saveProviderBtn').addEventListener('click', function() {
        saveProviderChanges();
    });

    // Setup delete provider button
    document.getElementById('deleteProviderBtn').addEventListener('click', function() {
        const providerName = document.getElementById('editProviderName').value;
        if (confirm(`Are you sure you want to delete the ${providerName} provider?`)) {
            sendMessage('delete_provider', { data: { provider_name: providerName } });
            editProviderModal.hide();
        }
    });
}

function saveProviderChanges() {
    const providerName = document.getElementById('editProviderName').value;
    const apiKey = document.getElementById('editProviderApiKey').value;
    const baseUrl = document.getElementById('editProviderBaseUrl').value;
    const rpm = parseInt(document.getElementById('editRequestsPerMinute').value) || 0;
    const interval = parseInt(document.getElementById('editMinIntervalMs').value) || 0;
    const tokens = parseInt(document.getElementById('editTokensPerMinute').value) || 0;

    const data = {
        provider_name: providerName
    };

    if (apiKey) {
        data.api_key = apiKey;
    }
    if (baseUrl) {
        data.base_url = baseUrl;
    }
    if (rpm > 0 || interval > 0 || tokens > 0) {
        data.requests_per_minute = rpm;
        data.min_interval_ms = interval;
        data.tokens_per_minute = tokens;
    }

    sendMessage('update_provider', { data });
    editProviderModal.hide();
}

// Store current providers data for editing
let currentProviders = [];

function editProvider(providerName) {
    const provider = currentProviders.find(p => p.name === providerName);
    if (!provider) return;

    // Fill in the edit form
    document.getElementById('editProviderName').value = provider.name;
    document.getElementById('editProviderNameDisplay').value = provider.display_name;
    document.getElementById('editProviderApiKey').value = '';
    
    // Show/hide base URL field
    const baseUrlField = document.getElementById('editBaseUrlField');
    if (provider.name === 'openai-compatible' || provider.base_url) {
        baseUrlField.style.display = 'block';
        document.getElementById('editProviderBaseUrl').value = provider.base_url || '';
    } else {
        baseUrlField.style.display = 'none';
    }

    // Fill in rate limits
    if (provider.rate_limit) {
        document.getElementById('editRequestsPerMinute').value = provider.rate_limit.requests_per_minute || '';
        document.getElementById('editMinIntervalMs').value = provider.rate_limit.min_interval_millis || '';
        document.getElementById('editTokensPerMinute').value = provider.rate_limit.tokens_per_minute || '';
    } else {
        document.getElementById('editRequestsPerMinute').value = '';
        document.getElementById('editMinIntervalMs').value = '';
        document.getElementById('editTokensPerMinute').value = '';
    }

    // Show the edit provider modal
    editProviderModal.show();
}

function renderProviders(providers) {
    const list = document.getElementById('providers-list');
    
    // Store providers for editing
    currentProviders = providers || [];
    
    if (!providers || providers.length === 0) {
        list.innerHTML = '<div class="alert alert-info">No providers configured.</div>';
        return;
    }

    let html = '<div class="list-group">';
    providers.forEach(provider => {
        const rateLimitText = provider.rate_limit ? 
            `${provider.rate_limit.requests_per_minute || '∞'} req/min` : 'unlimited';
        
        html += `
            <div class="list-group-item">
                <div class="d-flex justify-content-between align-items-center">
                    <div>
                        <strong>${escapeHtml(provider.display_name)}</strong>
                        <div class="small text-muted">
                            API Key: ${escapeHtml(provider.api_key || 'not set')} • ${provider.model_count} models • ${rateLimitText}
                        </div>
                    </div>
                    <button class="btn btn-sm btn-outline-primary" onclick="editProvider('${escapeHtml(provider.name)}')">
                        <i class="bi bi-pencil"></i> Edit
                    </button>
                </div>
            </div>
        `;
    });
    html += '</div>';
    list.innerHTML = html;
}

// ==================== Model Selection ====================

function loadModels() {
    sendMessage('get_models');
}

function setupModelsForm() {
    document.getElementById('modelsForm').addEventListener('submit', function(e) {
        e.preventDefault();
        
        const orchestrationModel = document.getElementById('orchestrationModel').value;
        const summarizationModel = document.getElementById('summarizationModel').value;
        const planningModel = document.getElementById('planningModel').value;
        const safetyModel = document.getElementById('safetyModel').value;

        if (orchestrationModel) {
            sendMessage('set_model', { data: { role: 'orchestration', model_id: orchestrationModel } });
        }
        if (summarizationModel) {
            sendMessage('set_model', { data: { role: 'summarize', model_id: summarizationModel } });
        }
        if (planningModel) {
            sendMessage('set_model', { data: { role: 'planning', model_id: planningModel } });
        }
        if (safetyModel) {
            sendMessage('set_model', { data: { role: 'safety', model_id: safetyModel } });
        }

        modelsModal.hide();
    });

    // Setup search inputs
    ['orchestration', 'summarization', 'planning', 'safety'].forEach(modelType => {
        const searchInput = document.getElementById(`model-search-${modelType}`);
        if (searchInput) {
            let debounceTimeout;
            searchInput.addEventListener('input', function(e) {
                clearTimeout(debounceTimeout);
                debounceTimeout = setTimeout(() => {
                    searchQueries[modelType] = e.target.value;
                    renderModelSelector(modelType, allModels);
                }, 200);
            });
        }

        // Setup show details toggle
        const showDetailsToggle = document.getElementById(`show-details-${modelType}`);
        if (showDetailsToggle) {
            showDetailsToggle.addEventListener('change', function() {
                showDetails[modelType] = this.checked;
                renderModelSelector(modelType, allModels);
            });
        }
    });
}

// Store models data globally for filtering
let allModels = [];
let selectedModels = {
    orchestration: '',
    summarization: '',
    planning: '',
    safety: ''
};
let modelProviders = new Set();
let showDetails = {
    orchestration: false,
    summarization: false,
    planning: false,
    safety: false
};
let activeProviderFilters = {
    orchestration: 'all',
    summarization: 'all',
    planning: 'all',
    safety: 'all'
};
let searchQueries = {
    orchestration: '',
    summarization: '',
    planning: '',
    safety: ''
};

function renderModels(models) {
    // Store models globally
    allModels = models || [];

    if (!models || models.length === 0) {
        ['orchestration', 'summarization', 'planning', 'safety'].forEach(modelType => {
            const grid = document.getElementById(`model-grid-${modelType}`);
            const count = document.getElementById(`model-count-${modelType}`);
            if (grid) {
                grid.innerHTML = '<div class="text-center text-muted py-5">No models available</div>';
            }
            if (count) {
                count.textContent = '0 models';
            }
        });
        return;
    }

    // Extract unique providers
    modelProviders = new Set(models.map(m => m.provider));

    // Render model selectors for each type
    renderModelSelector('orchestration', models);
    renderModelSelector('summarization', models);
    renderModelSelector('planning', models);
    renderModelSelector('safety', models);

    // Add provider filter buttons
    addProviderFilters('orchestration', modelProviders);
    addProviderFilters('summarization', modelProviders);
    addProviderFilters('planning', modelProviders);
    addProviderFilters('safety', modelProviders);
}

function initCurrentModels(currentModels) {
    // Initialize selected models from server state
    if (currentModels) {
        selectedModels.orchestration = currentModels.orchestration || '';
        selectedModels.summarization = currentModels.summarization || '';
        selectedModels.planning = currentModels.planning || '';
        selectedModels.safety = currentModels.safety || '';

        // Update hidden inputs
        const orchInput = document.getElementById('orchestrationModel');
        if (orchInput) orchInput.value = selectedModels.orchestration;
        
        const sumInput = document.getElementById('summarizationModel');
        if (sumInput) sumInput.value = selectedModels.summarization;
        
        const planInput = document.getElementById('planningModel');
        if (planInput) planInput.value = selectedModels.planning;
        
        const safetyInput = document.getElementById('safetyModel');
        if (safetyInput) safetyInput.value = selectedModels.safety;

        // Update selected model displays
        ['orchestration', 'summarization', 'planning', 'safety'].forEach(modelType => {
            updateSelectedModelDisplay(modelType);
        });
    }
}

function renderModelSelector(modelType, models) {
    const grid = document.getElementById(`model-grid-${modelType}`);
    const count = document.getElementById(`model-count-${modelType}`);
    
    if (!grid) return;

    // Filter models based on search query and provider filter
    const filteredModels = filterModels(modelType, models);

    // Update count
    if (count) {
        count.textContent = `${filteredModels.length} model${filteredModels.length !== 1 ? 's' : ''}`;
    }

    if (filteredModels.length === 0) {
        grid.innerHTML = '<div class="text-center text-muted py-5">No matching models</div>';
        return;
    }

    // Render model cards
    const cardsHtml = filteredModels.map(model => {
        const isSelected = selectedModels[modelType] === model.id;
        const showDets = showDetails[modelType];
        return `
            <div class="model-card card mb-2 cursor-pointer ${isSelected ? 'border-primary bg-light' : 'border'} ${showDets ? 'model-card-expanded' : ''}" 
                 data-model-id="${escapeHtml(model.id)}" 
                 data-model-name="${escapeHtml(model.name)}" 
                 data-provider="${escapeHtml(model.provider)}" 
                 data-model-type="${modelType}" 
                 tabindex="0" 
                 role="button" 
                 aria-pressed="${isSelected ? 'true' : 'false'}"
                 onclick="selectModel('${modelType}', '${escapeHtml(model.id)}')"
                 onkeydown="if(event.key === 'Enter' || event.key === ' ') { event.preventDefault(); selectModel('${modelType}', '${escapeHtml(model.id)}'); }">
                <div class="card-body py-2 px-3">
                    <div class="d-flex justify-content-between align-items-start">
                        <div class="flex-grow-1">
                            <div class="d-flex align-items-center gap-2">
                                <h6 class="card-title mb-0 fw-semibold">${escapeHtml(model.name)}</h6>
                                ${isSelected ? '<span class="badge bg-primary">Selected</span>' : ''}
                            </div>
                            <div class="small text-muted mt-1">
                                <span class="badge bg-secondary bg-opacity-10 text-secondary border">
                                    ${escapeHtml(model.provider)}
                                </span>
                                <span class="ms-2 font-monospace text-muted" style="font-size: 0.8em;">
                                    ${escapeHtml(model.id)}
                                </span>
                            </div>
                            ${showDets ? `
                                <div class="mt-2 pt-2 border-top small">
                                    <div class="row">
                                        <div class="col-6">
                                            <strong class="text-muted">Provider:</strong> ${escapeHtml(model.provider)}
                                        </div>
                                        <div class="col-6">
                                            <strong class="text-muted">Model ID:</strong>
                                            <code class="text-muted">${escapeHtml(model.id)}</code>
                                        </div>
                                    </div>
                                </div>
                            ` : ''}
                        </div>
                        <div class="ms-2">
                            ${isSelected ? '<i class="bi bi-check-circle-fill text-primary fs-5"></i>' : '<i class="bi bi-circle text-muted fs-5"></i>'}
                        </div>
                    </div>
                </div>
            </div>
        `;
    }).join('');

    grid.innerHTML = cardsHtml;

    // Update hidden input
    const hiddenInput = document.getElementById(`${modelType}Model`);
    if (hiddenInput) {
        hiddenInput.value = selectedModels[modelType] || '';
    }
}

function filterModels(modelType, models) {
    const searchQuery = searchQueries[modelType].toLowerCase();
    const providerFilter = activeProviderFilters[modelType];

    return models.filter(model => {
        // Filter by search query
        const matchesSearch = !searchQuery || 
            model.name.toLowerCase().includes(searchQuery) || 
            model.id.toLowerCase().includes(searchQuery);

        // Filter by provider
        const matchesProvider = providerFilter === 'all' || model.provider === providerFilter;

        return matchesSearch && matchesProvider;
    });
}

function addProviderFilters(modelType, providers) {
    const container = document.getElementById(`provider-filters-${modelType}`);
    if (!container) return;

    // Clear existing buttons
    container.innerHTML = '';

    // Create All Providers button (always create fresh to ensure onclick works)
    const allBtn = document.createElement('button');
    allBtn.type = 'button';
    allBtn.className = 'btn btn-outline-secondary active';
    allBtn.dataset.provider = 'all';
    allBtn.dataset.modelType = modelType;
    allBtn.textContent = 'All Providers';
    allBtn.onclick = () => setProviderFilter(modelType, 'all');
    container.appendChild(allBtn);

    // Add provider buttons
    providers.forEach(provider => {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'btn btn-outline-secondary';
        btn.dataset.provider = provider;
        btn.dataset.modelType = modelType;
        btn.textContent = escapeHtml(provider);
        btn.onclick = () => setProviderFilter(modelType, provider);
        container.appendChild(btn);
    });
}

function selectModel(modelType, modelId) {
    selectedModels[modelType] = modelId;
    renderModelSelector(modelType, allModels);
    updateSelectedModelDisplay(modelType);
}

function updateSelectedModelDisplay(modelType) {
    const display = document.getElementById(`selected-model-display-${modelType}`);
    const nameSpan = document.getElementById(`selected-model-name-${modelType}`);
    
    if (!display || !nameSpan) return;

    const selectedModel = allModels.find(m => m.id === selectedModels[modelType]);
    if (selectedModel) {
        nameSpan.textContent = selectedModel.name;
        display.style.display = 'block';
    } else {
        display.style.display = 'none';
    }
}

function setProviderFilter(modelType, provider) {
    activeProviderFilters[modelType] = provider;

    // Update button styles
    const container = document.getElementById(`provider-filters-${modelType}`);
    if (container) {
        container.querySelectorAll('button').forEach(btn => {
            if (btn.dataset.provider === provider) {
                btn.classList.add('active');
            } else {
                btn.classList.remove('active');
            }
        });
    }

    renderModelSelector(modelType, allModels);
}

function toggleShowDetails(modelType) {
    showDetails[modelType] = !showDetails[modelType];
    renderModelSelector(modelType, allModels);
}

// ==================== Search Configuration ====================

function loadSearchConfig() {
    sendMessage('get_search_config');
}

function setupSearchForm() {
    document.getElementById('searchForm').addEventListener('submit', function(e) {
        e.preventDefault();
        
        const provider = document.getElementById('searchProvider').value;
        const apiKey = document.getElementById('searchApiKey').value;

        sendMessage('set_search_config', {
            data: { provider, api_key: apiKey }
        });

        searchModal.hide();
    });
}

function renderSearchConfig(config) {
    document.getElementById('searchProvider').value = config.provider || '';
    document.getElementById('searchApiKey').value = config.api_key || '';
}

// ==================== Password Management ====================

function setupPasswordForm() {
    document.getElementById('passwordForm').addEventListener('submit', function(e) {
        e.preventDefault();
        
        const newPassword = document.getElementById('newPassword').value;
        const confirmPassword = document.getElementById('confirmPassword').value;
        const errorDiv = document.getElementById('passwordError');

        if (newPassword !== confirmPassword) {
            errorDiv.textContent = 'Passwords do not match';
            errorDiv.style.display = 'block';
            return;
        }

        errorDiv.style.display = 'none';

        sendMessage('set_password', {
            data: { password: newPassword }
        });

        passwordModal.hide();
    });
}

// ==================== MCP Server Management ====================

function loadMCPServers() {
    sendMessage('get_mcp_servers');
}

function showAddMCP(mcpType) {
    const form = document.getElementById('add-mcp-form');
    const typeInput = document.getElementById('mcpType');
    const typeDisplay = document.getElementById('mcpTypeDisplay');
    
    typeInput.value = mcpType;
    typeDisplay.value = mcpType.charAt(0).toUpperCase() + mcpType.slice(1);
    
    // Hide all field sets
    document.getElementById('openapi-fields').style.display = 'none';
    document.getElementById('command-fields').style.display = 'none';
    document.getElementById('openai-fields').style.display = 'none';
    
    // Show relevant field set
    document.getElementById(mcpType + '-fields').style.display = 'block';
    
    // Clear inputs
    document.getElementById('mcpServerName').value = '';
    document.getElementById('mcpSpecPath').value = '';
    document.getElementById('mcpServiceURL').value = '';
    document.getElementById('mcpCommand').value = '';
    document.getElementById('mcpWorkingDir').value = '';
    document.getElementById('mcpTimeout').value = '60';
    document.getElementById('mcpModel').value = '';
    document.getElementById('mcpBaseURL').value = '';
    
    form.style.display = 'block';
}

function hideAddMCP() {
    document.getElementById('add-mcp-form').style.display = 'none';
}

function setupMCPForm() {
    document.getElementById('mcpForm').addEventListener('submit', function(e) {
        e.preventDefault();
        
        const mcpType = document.getElementById('mcpType').value;
        const serverName = document.getElementById('mcpServerName').value;
        
        const data = {
            name: serverName,
            type: mcpType
        };

        switch (mcpType) {
            case 'openapi':
                data.spec_path = document.getElementById('mcpSpecPath').value;
                data.url = document.getElementById('mcpServiceURL').value;
                break;
            case 'command':
                data.exec = document.getElementById('mcpCommand').value;
                data.working_dir = document.getElementById('mcpWorkingDir').value;
                data.timeout_seconds = parseInt(document.getElementById('mcpTimeout').value);
                break;
            case 'openai':
                data.model = document.getElementById('mcpModel').value;
                data.base_url = document.getElementById('mcpBaseURL').value;
                break;
        }

        sendMessage('add_mcp_server', { data });
    });
}

function renderMCPServers(servers) {
    const list = document.getElementById('mcp-servers-list');
    
    if (!servers || servers.length === 0) {
        list.innerHTML = '<div class="alert alert-info">No MCP servers configured.</div>';
        return;
    }

    let html = '<div class="list-group">';
    servers.forEach(server => {
        const statusBadge = server.disabled ? 
            '<span class="badge bg-secondary">Disabled</span>' : 
            '<span class="badge bg-success">Enabled</span>';
        
        const configDetails = Object.entries(server.config || {})
            .map(([key, value]) => `<div><small>${escapeHtml(key)}: ${escapeHtml(String(value))}</small></div>`)
            .join('');
        
        html += `
            <div class="list-group-item">
                <div class="d-flex justify-content-between align-items-start">
                    <div>
                        <strong>${escapeHtml(server.name)}</strong>
                        <span class="ms-2">${statusBadge}</span>
                        <div class="small text-muted">Type: ${escapeHtml(server.type)}</div>
                        ${configDetails}
                    </div>
                    <div class="btn-group btn-group-sm">
                        <button class="btn btn-outline-${server.disabled ? 'success' : 'warning'}" onclick="toggleMCPServer('${escapeHtml(server.name)}')">
                            <i class="bi bi-${server.disabled ? 'play' : 'pause'}"></i>
                        </button>
                        <button class="btn btn-outline-danger" onclick="deleteMCPServer('${escapeHtml(server.name)}')">
                            <i class="bi bi-trash"></i>
                        </button>
                    </div>
                </div>
            </div>
        `;
    });
    html += '</div>';
    list.innerHTML = html;
}

function toggleMCPServer(serverName) {
    sendMessage('toggle_mcp_server', { data: { name: serverName } });
}

function deleteMCPServer(serverName) {
    if (confirm(`Are you sure you want to delete MCP server '${serverName}'?`)) {
        sendMessage('delete_mcp_server', { data: { name: serverName } });
    }
}

// ==================== Enhanced Message Handling ====================

// Extend handleMessage to handle menu-related messages
const originalHandleMessage = handleMessage;

handleMessage = function(msg) {
    switch(msg.type) {
        case 'providers':
            renderProviders(msg.data.providers);
            break;
        case 'models':
            // Initialize current models BEFORE rendering so selection state is correct
            if (msg.data.current_models) {
                initCurrentModels(msg.data.current_models);
            }
            renderModels(msg.data.models);
            break;
        case 'search_config':
            renderSearchConfig(msg.data);
            break;
        case 'mcp_servers':
            renderMCPServers(msg.data.servers);
            break;
        case 'error':
            addMessage("Error", msg.content, "danger");
            // Reload provider/server list after error
            if (msg.content.includes('provider') || msg.content.includes('model')) {
                loadProviders();
            }
            if (msg.content.includes('MCP')) {
                loadMCPServers();
            }
            break;
        case 'system':
            // Handle system messages for successful operations
            if (msg.content.includes('Successfully added') || 
                msg.content.includes('Successfully updated') || 
                msg.content.includes('Successfully set') || 
                msg.content.includes('Successfully deleted') ||
                msg.content.includes('Successfully enabled') ||
                msg.content.includes('Successfully disabled')) {
                addMessage("System", msg.content, "success");
                // Reload relevant data
                if (msg.content.includes('provider')) {
                    loadProviders();
                }
                if (msg.content.includes('MCP')) {
                    loadMCPServers();
                }
                if (msg.content.includes('model')) {
                    loadModels();
                }
            } else {
                // Call original handler for other system messages
                originalHandleMessage(msg);
            }
            break;
        default:
            // Call original handler for other message types
            originalHandleMessage(msg);
    }
};

// ==================== Authorization Dialog ====================

let authorizationModal = null;
let questionModal = null;

// Initialize authorization and question modals when Bootstrap is ready
document.addEventListener('DOMContentLoaded', function() {
    const authModalEl = document.getElementById('authorizationModal');
    if (authModalEl) {
        authorizationModal = new bootstrap.Modal(authModalEl);
        
        // Set up button handlers
        document.getElementById('authApproveBtn').addEventListener('click', function() {
            sendAuthorizationResponse(true);
        });
        
        document.getElementById('authDenyBtn').addEventListener('click', function() {
            sendAuthorizationResponse(false);
        });
    }
    
    // Initialize question modal
    const questionModalEl = document.getElementById('questionModal');
    if (questionModalEl) {
        questionModal = new bootstrap.Modal(questionModalEl);
        
        // Set up submit button handler
        document.getElementById('questionSubmitBtn').addEventListener('click', function() {
            submitQuestionResponse();
        });
        
        // Allow Enter key to submit in single-line mode
        document.getElementById('questionAnswerInput').addEventListener('keydown', function(e) {
            if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                submitQuestionResponse();
            }
        });
    }
});

function handleAuthorizationRequest(msg) {
    if (!authorizationModal) {
        console.error('Authorization modal not initialized');
        // Auto-deny if modal not available
        sendMessage('authorization_response', {
            auth_id: msg.auth_id,
            approved: false
        });
        return;
    }
    
    // Populate modal fields
    document.getElementById('authId').value = msg.auth_id || '';
    document.getElementById('authToolName').textContent = msg.tool_name || 'Unknown';
    
    // Set reason (if provided)
    const reasonSection = document.getElementById('authReasonSection');
    const reasonEl = document.getElementById('authReason');
    if (msg.reason) {
        reasonEl.textContent = msg.reason;
        reasonSection.style.display = 'block';
    } else {
        reasonSection.style.display = 'none';
    }
    
    // Set parameters (if provided)
    const paramsSection = document.getElementById('authParamsSection');
    const paramsEl = document.getElementById('authParameters');
    if (msg.parameters && Object.keys(msg.parameters).length > 0) {
        try {
            paramsEl.textContent = JSON.stringify(msg.parameters, null, 2);
            paramsSection.style.display = 'block';
        } catch (e) {
            paramsSection.style.display = 'none';
        }
    } else {
        paramsSection.style.display = 'none';
    }
    
    // Show the modal
    authorizationModal.show();

    // Send ack to backend confirming the dialog is displayed
    sendMessage('authorization_ack', { auth_id: msg.auth_id });
}

function sendAuthorizationResponse(approved) {
    const authId = document.getElementById('authId').value;
    if (!authId) {
        console.error('No authorization ID found');
        return;
    }
    
    sendMessage('authorization_response', {
        auth_id: authId,
        approved: approved
    });
    
    // Hide the modal
    if (authorizationModal) {
        authorizationModal.hide();
    }
}

// ==================== Question Dialog Functions ====================

function handleQuestionRequest(msg) {
    if (!questionModal) {
        questionModal = new bootstrap.Modal(document.getElementById('questionModal'));
    }
    
    // Store question ID and mode
    document.getElementById('questionId').value = msg.question_id || '';
    document.getElementById('questionMultiMode').value = msg.multi_mode ? 'true' : 'false';
    
    const singleSection = document.getElementById('singleQuestionSection');
    const multiSection = document.getElementById('multiQuestionSection');
    
    if (msg.multi_mode && msg.questions && msg.questions.length > 0) {
        // Multi-question mode
        singleSection.style.display = 'none';
        multiSection.style.display = 'block';
        
        // Build multi-question form
        const container = document.getElementById('multiQuestionsContainer');
        container.innerHTML = '';
        
        msg.questions.forEach((q, idx) => {
            const questionDiv = document.createElement('div');
            questionDiv.className = 'mb-3';
            
            const label = document.createElement('label');
            label.className = 'form-label fw-bold';
            label.textContent = q.question;
            questionDiv.appendChild(label);
            
            if (q.options && q.options.length > 0) {
                // Multiple choice
                q.options.forEach((opt, optIdx) => {
                    const checkDiv = document.createElement('div');
                    checkDiv.className = 'form-check';
                    
                    const input = document.createElement('input');
                    input.className = 'form-check-input';
                    input.type = 'radio';
                    input.name = `question_${idx}`;
                    input.id = `question_${idx}_option_${optIdx}`;
                    input.value = opt;
                    
                    const optLabel = document.createElement('label');
                    optLabel.className = 'form-check-label';
                    optLabel.htmlFor = input.id;
                    optLabel.textContent = opt;
                    
                    checkDiv.appendChild(input);
                    checkDiv.appendChild(optLabel);
                    questionDiv.appendChild(checkDiv);
                });
            } else {
                // Text input
                const input = document.createElement('textarea');
                input.className = 'form-control';
                input.name = `question_${idx}`;
                input.id = `question_${idx}_input`;
                input.rows = 2;
                input.placeholder = 'Enter your answer...';
                questionDiv.appendChild(input);
            }
            
            container.appendChild(questionDiv);
        });
    } else {
        // Single question mode
        singleSection.style.display = 'block';
        multiSection.style.display = 'none';
        
        // Set question text
        document.getElementById('questionText').textContent = msg.question || '';
        
        // Handle options if provided
        const optionsContainer = document.getElementById('questionOptions');
        const inputSection = document.getElementById('questionInputSection');
        
        if (msg.questions && msg.questions.length > 0 && msg.questions[0].options && msg.questions[0].options.length > 0) {
            // Show options as radio buttons
            optionsContainer.innerHTML = '';
            msg.questions[0].options.forEach((opt, idx) => {
                const checkDiv = document.createElement('div');
                checkDiv.className = 'form-check';
                
                const input = document.createElement('input');
                input.className = 'form-check-input';
                input.type = 'radio';
                input.name = 'questionOption';
                input.id = `questionOption_${idx}`;
                input.value = opt;
                
                const optLabel = document.createElement('label');
                optLabel.className = 'form-check-label';
                optLabel.htmlFor = input.id;
                optLabel.textContent = opt;
                
                checkDiv.appendChild(input);
                checkDiv.appendChild(optLabel);
                optionsContainer.appendChild(checkDiv);
            });
            optionsContainer.style.display = 'block';
            inputSection.style.display = 'none';
        } else {
            // Show text input
            optionsContainer.style.display = 'none';
            inputSection.style.display = 'block';
            document.getElementById('questionAnswerInput').value = '';
        }
    }
    
    // Show the modal
    questionModal.show();
}

function submitQuestionResponse() {
    const questionId = document.getElementById('questionId').value;
    const multiMode = document.getElementById('questionMultiMode').value === 'true';
    
    if (!questionId) {
        console.error('No question ID found');
        return;
    }
    
    if (multiMode) {
        // Collect multi-question answers
        const container = document.getElementById('multiQuestionsContainer');
        const questionDivs = container.querySelectorAll('.mb-3');
        const answers = {};
        
        questionDivs.forEach((div, idx) => {
            // Check for radio buttons
            const selectedRadio = div.querySelector('input[type="radio"]:checked');
            if (selectedRadio) {
                answers[`question_${idx}`] = selectedRadio.value;
            } else {
                // Check for text input
                const textInput = div.querySelector('textarea');
                if (textInput) {
                    answers[`question_${idx}`] = textInput.value;
                }
            }
        });
        
        sendMessage('question_response', {
            question_id: questionId,
            answers_map: answers
        });
    } else {
        // Single question mode
        let answer = '';
        
        // Check for selected radio option
        const selectedRadio = document.querySelector('input[name="questionOption"]:checked');
        if (selectedRadio) {
            answer = selectedRadio.value;
        } else {
            // Get text input
            answer = document.getElementById('questionAnswerInput').value;
        }
        
        sendMessage('question_response', {
            question_id: questionId,
            answer: answer
        });
    }
    
    // Hide the modal
    if (questionModal) {
        questionModal.hide();
    }
}

// ==================== Utility Functions ====================

// Escape HTML (already defined, but kept for reference)
function escapeHtml(text) {
    if (text === null || text === undefined) return '';
    const map = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' };
    return String(text).replace(/[&<>"']/g, function(m) { return map[m]; });
}