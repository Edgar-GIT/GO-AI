const state = {
  bootstrap: null,
  chats: [],
  activeChatId: null,
  activeChat: null,
  trainingQueue: null,
  pendingAttachments: [],
  sending: false,
  typingIndicator: null,
};

const elements = {
  newChatButton: document.querySelector("#new-chat-button"),
  refreshButton: document.querySelector("#refresh-button"),
  chatSearch: document.querySelector("#chat-search"),
  chatList: document.querySelector("#chat-list"),
  chatCount: document.querySelector("#chat-count"),
  mainTitle: document.querySelector("#main-title"),
  welcomeTitle: document.querySelector("#welcome-title"),
  welcomeSubtitle: document.querySelector("#welcome-subtitle"),
  welcomeScreen: document.querySelector("#welcome-screen"),
  chatView: document.querySelector("#chat-view"),
  messageList: document.querySelector("#message-list"),
  messageInput: document.querySelector("#message-input"),
  sendButton: document.querySelector("#send-button"),
  trainChatButton: document.querySelector("#train-chat-button"),
  trainingCenterButton: document.querySelector("#training-center-button"),
  attachmentInput: document.querySelector("#attachment-input"),
  pendingAttachments: document.querySelector("#pending-attachments"),
  modelSelect: document.querySelector("#model-select"),
  modelStatusList: document.querySelector("#model-status-list"),
  quotaChip: document.querySelector("#quota-chip"),
  composerStatus: document.querySelector("#composer-status"),
  trainingDialog: document.querySelector("#training-dialog"),
  trainingCloseButton: document.querySelector("#training-close-button"),
  trainingBaseModel: document.querySelector("#training-base-model"),
  trainingEpochs: document.querySelector("#training-epochs"),
  trainingLearningRate: document.querySelector("#training-learning-rate"),
  trainingBatchSize: document.querySelector("#training-batch-size"),
  trainingStartButton: document.querySelector("#training-start-button"),
  trainingRefreshButton: document.querySelector("#training-refresh-button"),
  trainingStatusList: document.querySelector("#training-status-list"),
  messageTemplate: document.querySelector("#message-template"),
};

document.addEventListener("DOMContentLoaded", () => {
  bindEvents();
  loadBootstrap();
});

function bindEvents() {
  elements.newChatButton.addEventListener("click", async () => {
    const chat = await createChat();
    if (chat) {
      await openChat(chat.id);
      elements.messageInput.focus();
    }
  });

  elements.refreshButton.addEventListener("click", async () => {
    await refreshChats();
    if (state.activeChatId) {
      await openChat(state.activeChatId);
    }
    await refreshTrainingStatus(false);
  });

  elements.trainChatButton.addEventListener("click", openTrainingDialog);
  elements.trainingCenterButton.addEventListener("click", openTrainingDialog);
  elements.trainingCloseButton.addEventListener("click", () => {
    elements.trainingDialog.close();
  });
  elements.trainingRefreshButton.addEventListener("click", () => refreshTrainingStatus(true));
  elements.trainingStartButton.addEventListener("click", startManualTraining);

  elements.chatSearch.addEventListener("input", debounce(async event => {
    await refreshChats(event.target.value.trim());
  }, 220));

  elements.messageInput.addEventListener("input", autoResizeTextarea);
  elements.messageInput.addEventListener("keydown", async event => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      await sendMessage();
    }
  });

  elements.sendButton.addEventListener("click", sendMessage);
  elements.attachmentInput.addEventListener("change", handleAttachmentSelection);

  document.querySelectorAll(".quick-prompt").forEach(button => {
    button.addEventListener("click", () => {
      elements.messageInput.value = button.dataset.prompt || "";
      autoResizeTextarea();
      elements.messageInput.focus();
    });
  });
}

async function loadBootstrap() {
  setComposerStatus("Connecting to backend...");
  try {
    const data = await api("/api/app/bootstrap");
    state.bootstrap = data;
    state.chats = data.chats || [];
    renderBootstrap(data);
    renderChatList();
    await refreshTrainingStatus(false);
    if (state.chats.length > 0) {
      await openChat(state.chats[0].id);
    } else {
      renderWelcome();
    }
    setComposerStatus("Ready.");
  } catch (error) {
    console.error(error);
    setComposerStatus("Could not load Gopher AI. Check whether the backend is running.");
  }
}

function renderBootstrap(data) {
  const username = data?.user?.username || "friend";
  elements.mainTitle.textContent = "Welcome to Gopher AI";
  elements.welcomeTitle.textContent = `Welcome to Gopher AI, ${username}`;
  elements.welcomeSubtitle.textContent = "What are we gonna do today?";
  renderModels(data?.models);
  renderQuota(data?.quota);
}

async function refreshChats(search = elements.chatSearch.value.trim()) {
  const query = search ? `?search=${encodeURIComponent(search)}` : "";
  const data = await api(`/api/chats${query}`);
  state.chats = data.items || [];
  renderChatList();
}

function renderChatList() {
  elements.chatCount.textContent = String(state.chats.length);
  elements.chatList.innerHTML = "";

  if (state.chats.length === 0) {
    const empty = document.createElement("p");
    empty.className = "message-empty";
    empty.textContent = "No chats yet. Start a fresh conversation.";
    elements.chatList.appendChild(empty);
    return;
  }

  state.chats.forEach(chat => {
    const button = document.createElement("button");
    button.className = "chat-item";
    if (chat.id === state.activeChatId) {
      button.classList.add("active");
    }
    button.innerHTML = `
      <div class="chat-item-title">${escapeHTML(chat.title || "New Chat")}</div>
      <div class="chat-item-meta">${formatRelativeDate(chat.updatedAt)} · ${escapeHTML(chat.modelUsed || "No model")}</div>
      <div class="chat-item-meta">${escapeHTML(chat.lastMessagePreview || "No messages yet")}</div>
    `;
    button.addEventListener("click", () => openChat(chat.id));
    elements.chatList.appendChild(button);
  });
}

async function createChat() {
  try {
    const model = elements.modelSelect.value || state.bootstrap?.models?.primary || "local-llama";
    const chat = await api("/api/chats", {
      method: "POST",
      body: JSON.stringify({ model }),
    });
    await refreshChats();
    return chat;
  } catch (error) {
    console.error(error);
    setComposerStatus(error.message || "Could not create chat.");
    return null;
  }
}

async function openChat(chatId) {
  state.activeChatId = chatId;
  renderChatList();
  try {
    const chat = await api(`/api/chats/${chatId}`);
    state.activeChat = chat;
    renderActiveChat();
  } catch (error) {
    console.error(error);
    setComposerStatus(error.message || "Could not open chat.");
  }
}

function renderActiveChat() {
  const chat = state.activeChat;
  if (!chat) {
    renderWelcome();
    return;
  }

  elements.welcomeScreen.classList.add("hidden");
  elements.chatView.classList.remove("hidden");
  elements.mainTitle.textContent = chat.title || "New Chat";
  elements.messageList.innerHTML = "";

  (chat.messages || []).forEach(message => {
    elements.messageList.appendChild(buildMessageNode(message));
  });

  scrollMessagesToBottom();
}

function renderWelcome() {
  state.activeChat = null;
  state.activeChatId = null;
  elements.chatView.classList.add("hidden");
  elements.welcomeScreen.classList.remove("hidden");
  elements.mainTitle.textContent = "Welcome to Gopher AI";
  renderChatList();
}

function buildMessageNode(message) {
  const fragment = elements.messageTemplate.content.cloneNode(true);
  const article = fragment.querySelector(".message");
  const role = message.role || "assistant";
  article.classList.toggle("user", role === "user");
  fragment.querySelector(".message-role").textContent = role === "assistant" ? (message.model || "assistant") : "you";
  fragment.querySelector(".message-time").textContent = formatTime(message.timestamp);
  fragment.querySelector(".message-body").appendChild(renderMessageContent(message.content || ""));

  if (message.attachments?.length) {
    fragment.querySelector(".message-body").appendChild(renderAttachments(message.attachments));
  }

  const actions = fragment.querySelector(".message-actions");
  if (message.content) {
    const copyButton = document.createElement("button");
    copyButton.className = "copy-button";
    copyButton.textContent = "Copy";
    copyButton.addEventListener("click", async () => {
      await navigator.clipboard.writeText(message.content);
      setComposerStatus("Message copied.");
    });
    actions.appendChild(copyButton);
  }

  return fragment;
}

function renderMessageContent(content) {
  const wrapper = document.createElement("div");
  wrapper.className = "rendered-content";

  const blocks = content.split(/```/g);
  blocks.forEach((block, index) => {
    if (index % 2 === 1) {
      const pre = document.createElement("pre");
      const code = document.createElement("code");
      const normalized = block.replace(/^\w+\n/, "");
      code.textContent = normalized.trim();
      pre.appendChild(code);
      wrapper.appendChild(pre);
      return;
    }

    block
      .split(/\n{2,}/)
      .map(part => part.trim())
      .filter(Boolean)
      .forEach(part => {
        const paragraph = document.createElement("p");
        paragraph.innerHTML = escapeHTML(part).replace(/\n/g, "<br>");
        wrapper.appendChild(paragraph);
      });
  });

  if (!wrapper.childNodes.length) {
    const paragraph = document.createElement("p");
    paragraph.textContent = "";
    wrapper.appendChild(paragraph);
  }

  return wrapper;
}

function renderAttachments(attachments) {
  const container = document.createElement("div");
  container.className = "attachment-stack";

  attachments.forEach(attachment => {
    const link = document.createElement("a");
    link.className = "attachment-pill";
    link.href = `/api/attachments/${attachment.id}`;
    link.target = "_blank";
    link.rel = "noreferrer";
    link.textContent = attachment.filename;
    container.appendChild(link);
  });

  return container;
}

function renderModels(modelsSnapshot) {
  const items = modelsSnapshot?.availableModels || [];
  elements.modelSelect.innerHTML = "";
  elements.modelStatusList.innerHTML = "";

  items.forEach(item => {
    const option = document.createElement("option");
    option.value = item.id;
    option.textContent = `${item.label} · ${item.status}`;
    option.selected = item.id === (modelsSnapshot?.primary || "local-llama");
    elements.modelSelect.appendChild(option);

    const card = document.createElement("div");
    card.className = "status-item";
    card.innerHTML = `
      <div class="status-item-title">
        <strong>${escapeHTML(item.label)}</strong>
        <span class="status-state ${escapeClass(item.status)}">${escapeHTML(item.status)}</span>
      </div>
      <div class="status-item-copy">${escapeHTML(item.provider)} · ${escapeHTML(item.tier)} · ${escapeHTML(item.lifecycle)}</div>
    `;
    elements.modelStatusList.appendChild(card);
  });
}

async function openTrainingDialog() {
  elements.trainingDialog.showModal();
  await refreshTrainingStatus(false);
}

async function refreshTrainingStatus(announce) {
  try {
    const queue = await api("/api/training/status");
    state.trainingQueue = queue;
    renderTrainingStatus(queue);
    if (announce) {
      setComposerStatus("Training queue refreshed.");
    }
  } catch (error) {
    console.error(error);
    if (announce) {
      setComposerStatus(error.message || "Could not load training status.");
    }
  }
}

function renderTrainingStatus(queue) {
  const pending = queue?.pendingTasks || [];
  const completed = queue?.completedTasks || [];
  const items = [...pending, ...completed].slice().reverse();
  elements.trainingStatusList.innerHTML = "";

  if (!items.length) {
    const empty = document.createElement("p");
    empty.className = "message-empty";
    empty.textContent = "No training tasks yet.";
    elements.trainingStatusList.appendChild(empty);
    return;
  }

  items.forEach(item => {
    const card = document.createElement("article");
    card.className = "training-status-card";
    const adapter = item.adapterPath ? `<div class="status-item-copy">${escapeHTML(item.adapterPath)}</div>` : "";
    const error = item.lastError || item.applyError
      ? `<div class="training-error">${escapeHTML(item.lastError || item.applyError)}</div>`
      : "";
    card.innerHTML = `
      <div class="status-item-title">
        <strong>${escapeHTML(item.taskId || item.taskID || "training-task")}</strong>
        <span class="status-state ${escapeClass(item.status || "completed")}">${escapeHTML(item.status || "completed")}</span>
      </div>
      <div class="status-item-copy">${escapeHTML(item.type || "manual")} · ${escapeHTML(item.baseModel || "base model")}</div>
      <div class="status-item-copy">${Number(item.messageCount || 0)} pairs</div>
      ${adapter}
      ${error}
    `;
    elements.trainingStatusList.appendChild(card);
  });
}

async function startManualTraining() {
  if (!state.activeChatId) {
    setComposerStatus("Open a chat before starting manual training.");
    return;
  }

  elements.trainingStartButton.disabled = true;
  try {
    const task = await api("/api/training/manual", {
      method: "POST",
      body: JSON.stringify({
        chatIds: [state.activeChatId],
        epochs: Number(elements.trainingEpochs.value || 3),
        learningRate: Number(elements.trainingLearningRate.value || 0.0001),
        batchSize: Number(elements.trainingBatchSize.value || 1),
        baseModel: elements.trainingBaseModel.value.trim(),
      }),
    });
    await refreshTrainingStatus(false);
    setComposerStatus(`Training started: ${task.taskId}.`);
  } catch (error) {
    console.error(error);
    setComposerStatus(error.message || "Could not start manual training.");
  } finally {
    elements.trainingStartButton.disabled = false;
  }
}

function renderQuota(quota) {
  const used = quota?.gemini?.totalTokensUsedToday ?? 0;
  const limit = quota?.gemini?.totalTokensLimit ?? 0;
  elements.quotaChip.textContent = limit ? `${used} / ${limit} tokens` : "Quota unavailable";
}

async function sendMessage() {
  if (state.sending) {
    return;
  }

  const content = elements.messageInput.value.trim();
  if (!content && state.pendingAttachments.length === 0) {
    setComposerStatus("Type a message or attach a file first.");
    return;
  }

  let chatId = state.activeChatId;
  if (!chatId) {
    const chat = await createChat();
    if (!chat) {
      return;
    }
    chatId = chat.id;
    state.activeChatId = chatId;
  }

  state.sending = true;
  updateSendingState();
  showTypingIndicator();
  setComposerStatus("Gopher AI is thinking...");

  try {
    const payload = {
      content,
      attachmentIds: state.pendingAttachments.map(item => item.id),
      forceModel: elements.modelSelect.value || undefined,
    };

    const response = await api(`/api/chats/${chatId}/messages`, {
      method: "POST",
      body: JSON.stringify(payload),
    });

    state.activeChat = response.chat;
    elements.messageInput.value = "";
    state.pendingAttachments = [];
    elements.attachmentInput.value = "";
    autoResizeTextarea();
    renderPendingAttachments();
    await refreshChats();
    renderActiveChat();
    renderQuota(state.bootstrap?.quota);
    if (response.trainingTask?.taskId) {
      setComposerStatus(`Reply ready via ${response.modelUsed}. Training queued: ${response.trainingTask.taskId}.`);
    } else {
      setComposerStatus(`Reply ready via ${response.modelUsed}.`);
    }
  } catch (error) {
    console.error(error);
    setComposerStatus(error.message || "Could not send message.");
  } finally {
    removeTypingIndicator();
    state.sending = false;
    updateSendingState();
  }
}

async function handleAttachmentSelection(event) {
  const files = [...(event.target.files || [])];
  if (files.length === 0) {
    return;
  }

  setComposerStatus("Uploading attachments...");
  for (const file of files) {
    try {
      const formData = new FormData();
      formData.append("file", file);
      const attachment = await api("/api/attachments/upload", {
        method: "POST",
        body: formData,
        isMultipart: true,
      });
      state.pendingAttachments.push(attachment);
    } catch (error) {
      console.error(error);
      setComposerStatus(`${file.name}: ${error.message}`);
    }
  }

  renderPendingAttachments();
  setComposerStatus("Attachments ready.");
}

function renderPendingAttachments() {
  elements.pendingAttachments.innerHTML = "";
  state.pendingAttachments.forEach(attachment => {
    const card = document.createElement("div");
    card.className = "pending-attachment";
    card.innerHTML = `
      <div>
        <strong>${escapeHTML(attachment.filename)}</strong>
        <div class="message-time">${formatBytes(attachment.size)} · ${escapeHTML(attachment.mimeType)}</div>
      </div>
    `;

    const removeButton = document.createElement("button");
    removeButton.type = "button";
    removeButton.textContent = "Remove";
    removeButton.addEventListener("click", () => {
      state.pendingAttachments = state.pendingAttachments.filter(item => item.id !== attachment.id);
      renderPendingAttachments();
    });
    card.appendChild(removeButton);
    elements.pendingAttachments.appendChild(card);
  });
}

function updateSendingState() {
  elements.sendButton.disabled = state.sending;
  elements.sendButton.textContent = state.sending ? "Sending..." : "Send";
}

function showTypingIndicator() {
  removeTypingIndicator();
  const indicator = document.createElement("article");
  indicator.className = "message";
  indicator.id = "typing-indicator";
  indicator.innerHTML = `
    <div class="avatar"></div>
    <div class="message-card">
      <div class="message-meta">
        <span class="message-role">gopher ai</span>
        <span class="message-time">now</span>
      </div>
      <div class="typing-indicator"><span></span><span></span><span></span></div>
    </div>
  `;
  elements.messageList.appendChild(indicator);
  scrollMessagesToBottom();
  state.typingIndicator = indicator;
}

function removeTypingIndicator() {
  state.typingIndicator?.remove();
  state.typingIndicator = null;
}

function scrollMessagesToBottom() {
  elements.chatView.scrollTop = elements.chatView.scrollHeight;
}

function autoResizeTextarea() {
  elements.messageInput.style.height = "auto";
  elements.messageInput.style.height = `${Math.min(elements.messageInput.scrollHeight, 192)}px`;
}

function setComposerStatus(message) {
  elements.composerStatus.textContent = message;
}

async function api(path, options = {}) {
  const fetchOptions = {
    method: options.method || "GET",
    headers: options.isMultipart ? {} : { "Content-Type": "application/json", ...(options.headers || {}) },
    body: options.body,
  };

  const response = await fetch(path, fetchOptions);
  const isJSON = response.headers.get("content-type")?.includes("application/json");
  const payload = isJSON ? await response.json() : await response.text();
  if (!response.ok) {
    const message = payload?.error || payload || `Request failed with ${response.status}`;
    throw new Error(message);
  }
  return payload;
}

function debounce(fn, delay) {
  let timeout;
  return (...args) => {
    window.clearTimeout(timeout);
    timeout = window.setTimeout(() => fn(...args), delay);
  };
}

function formatTime(value) {
  if (!value) {
    return "now";
  }
  return new Date(value).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function formatRelativeDate(value) {
  if (!value) {
    return "just now";
  }
  const date = new Date(value);
  return `${date.toLocaleDateString()} ${date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
}

function formatBytes(bytes) {
  const value = Number(bytes || 0);
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(1)} KB`;
  }
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function escapeClass(value) {
  return String(value || "")
    .replace(/[^a-zA-Z0-9_-]/g, "-")
    .toLowerCase();
}
