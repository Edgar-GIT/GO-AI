const state = {
  bootstrap: null,
  chats: [],
  activeChatId: null,
  activeChat: null,
  trainingQueue: null,
  pendingAttachments: [],
  sending: false,
  typingIndicator: null,
  previousModelSelection: "gopher-ai",
  pendingGeminiModel: null,
  pendingGeminiReason: "",
  sidebarOpen: window.innerWidth > 960,
  autoRefreshTimer: null,
};

const elements = {
  appShell: document.querySelector("#app-shell"),
  sidebar: document.querySelector("#sidebar"),
  sidebarOverlay: document.querySelector("#sidebar-overlay"),
  sidebarToggle: document.querySelector("#sidebar-toggle"),
  newChatButton: document.querySelector("#new-chat-button"),
  chatSearch: document.querySelector("#chat-search"),
  chatList: document.querySelector("#chat-list"),
  chatCount: document.querySelector("#chat-count"),
  mainTitle: document.querySelector("#main-title"),
  welcomeTitle: document.querySelector("#welcome-title"),
  welcomeSubtitle: document.querySelector("#welcome-subtitle"),
  welcomeScreen: document.querySelector("#welcome-screen"),
  chatView: document.querySelector("#chat-view"),
  chatEmptyState: document.querySelector("#chat-empty-state"),
  messageList: document.querySelector("#message-list"),
  messageInput: document.querySelector("#message-input"),
  sendButton: document.querySelector("#send-button"),
  trainChatButton: document.querySelector("#train-chat-button"),
  trainingCenterButton: document.querySelector("#training-center-button"),
  attachmentInput: document.querySelector("#attachment-input"),
  pendingAttachments: document.querySelector("#pending-attachments"),
  modelSelect: document.querySelector("#model-select"),
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
  geminiDialog: document.querySelector("#gemini-dialog"),
  geminiCopy: document.querySelector("#gemini-copy"),
  geminiCloseButton: document.querySelector("#gemini-close-button"),
  geminiCancelButton: document.querySelector("#gemini-cancel-button"),
  geminiResetQuotaButton: document.querySelector("#gemini-reset-quota-button"),
  geminiUseSavedButton: document.querySelector("#gemini-use-saved-button"),
  geminiSaveButton: document.querySelector("#gemini-save-button"),
  geminiAPIKeyInput: document.querySelector("#gemini-api-key-input"),
  messageTemplate: document.querySelector("#message-template"),
  quickPrompts: [...document.querySelectorAll(".quick-prompt")],
};

document.addEventListener("DOMContentLoaded", () => {
  bindEvents();
  applySidebarState();
  handleViewportChange();
  loadBootstrap();
  startAutoRefresh();
});

function bindEvents() {
  window.addEventListener("resize", debounce(handleViewportChange, 120));
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) {
      backgroundRefresh();
    }
  });

  elements.sidebarToggle.addEventListener("click", () => {
    setSidebarOpen(!state.sidebarOpen);
  });
  elements.sidebarOverlay.addEventListener("click", () => {
    setSidebarOpen(false);
  });

  elements.newChatButton.addEventListener("click", async () => {
    const chat = await createChat();
    if (!chat) {
      return;
    }
    state.activeChatId = chat.id;
    state.activeChat = chat;
    renderChatList();
    renderActiveChat();
    elements.messageInput.focus();
    if (isNarrowLayout()) {
      setSidebarOpen(false);
    }
  });

  elements.trainChatButton.addEventListener("click", openTrainingDialog);
  elements.trainingCenterButton.addEventListener("click", openTrainingDialog);
  elements.trainingCloseButton.addEventListener("click", () => {
    elements.trainingDialog.close();
  });
  elements.trainingRefreshButton.addEventListener("click", () => refreshTrainingStatus(true));
  elements.trainingStartButton.addEventListener("click", startManualTraining);

  elements.modelSelect.addEventListener("focus", () => {
    state.previousModelSelection = elements.modelSelect.value || state.previousModelSelection;
  });
  elements.modelSelect.addEventListener("change", handleModelSelectionChange);

  elements.geminiCloseButton.addEventListener("click", () => closeGeminiDialog(false));
  elements.geminiCancelButton.addEventListener("click", () => closeGeminiDialog(false));
  elements.geminiResetQuotaButton.addEventListener("click", resetLocalGeminiQuota);
  elements.geminiUseSavedButton.addEventListener("click", useSavedGeminiKey);
  elements.geminiSaveButton.addEventListener("click", saveGeminiAPIKey);
  elements.geminiDialog.addEventListener("cancel", event => {
    event.preventDefault();
    closeGeminiDialog(false);
  });

  elements.chatSearch.addEventListener("input", debounce(async event => {
    await refreshChats(event.target.value.trim());
  }, 180));

  elements.messageInput.addEventListener("input", autoResizeTextarea);
  elements.messageInput.addEventListener("keydown", async event => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      await sendMessage();
    }
  });

  elements.sendButton.addEventListener("click", sendMessage);
  elements.attachmentInput.addEventListener("change", handleAttachmentSelection);

  elements.quickPrompts.forEach(button => {
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

    if (state.activeChatId && state.chats.some(chat => chat.id === state.activeChatId)) {
      await openChat(state.activeChatId);
    } else if (state.chats.length > 0) {
      await openChat(state.chats[0].id);
    } else {
      renderWelcome();
    }

    setComposerStatus("Ready.");
  } catch (error) {
    console.error(error);
    renderWelcome();
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

  if (!search && state.activeChatId && !state.chats.some(chat => chat.id === state.activeChatId)) {
    state.activeChatId = null;
    state.activeChat = null;
  }

  renderChatList();
}

function renderChatList() {
  elements.chatCount.textContent = String(state.chats.length);
  elements.chatList.innerHTML = "";

  if (state.chats.length === 0) {
    const empty = document.createElement("div");
    empty.className = "sidebar-empty";
    empty.innerHTML = `
      <div class="sidebar-empty-blob"><span class="assistant-blob"></span></div>
      <strong>No chats yet</strong>
      <span>Start a new conversation to see it here.</span>
    `;
    elements.chatList.appendChild(empty);
    return;
  }

  state.chats.forEach(chat => {
    const shell = document.createElement("article");
    shell.className = "chat-item-shell";
    if (chat.id === state.activeChatId) {
      shell.classList.add("active");
    }
    const button = document.createElement("button");
    button.type = "button";
    button.className = "chat-item-button";
    button.innerHTML = `
      <div class="chat-item-title">${escapeHTML(chat.title || "New Chat")}</div>
      <div class="chat-item-meta">${formatRelativeDate(chat.updatedAt)}</div>
      <div class="chat-item-preview">${escapeHTML(chat.lastMessagePreview || "No messages yet")}</div>
    `;
    button.addEventListener("click", () => openChat(chat.id));

    const deleteButton = document.createElement("button");
    deleteButton.type = "button";
    deleteButton.className = "chat-delete-button";
    deleteButton.setAttribute("aria-label", `Delete ${chat.title || "chat"}`);
    deleteButton.textContent = "Delete";
    deleteButton.addEventListener("click", async event => {
      event.stopPropagation();
      await deleteChat(chat.id);
    });

    shell.appendChild(button);
    shell.appendChild(deleteButton);
    elements.chatList.appendChild(shell);
  });
}

async function createChat() {
  try {
    const model = elements.modelSelect.value || state.bootstrap?.models?.primary || "gopher-ai";
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
    if (isNarrowLayout()) {
      setSidebarOpen(false);
    }
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

  const messages = chat.messages || [];
  if (messages.length === 0) {
    elements.chatEmptyState.classList.remove("hidden");
    elements.messageList.classList.add("hidden");
    return;
  }

  elements.chatEmptyState.classList.add("hidden");
  elements.messageList.classList.remove("hidden");

  messages.forEach(message => {
    elements.messageList.appendChild(buildMessageNode(message));
  });

  scrollMessagesToBottom();
}

function renderWelcome() {
  elements.welcomeScreen.classList.remove("hidden");
  elements.chatView.classList.add("hidden");
  elements.chatEmptyState.classList.add("hidden");
  elements.messageList.classList.remove("hidden");
  elements.mainTitle.textContent = "Welcome to Gopher AI";
}

function buildMessageNode(message) {
  const fragment = elements.messageTemplate.content.cloneNode(true);
  const article = fragment.querySelector(".message");
  const avatar = fragment.querySelector(".avatar");
  const role = message.role || "assistant";

  article.classList.add(role === "assistant" ? "assistant" : "user");

  if (role === "assistant") {
    avatar.innerHTML = `<span class="assistant-blob"></span>`;
  } else {
    avatar.classList.add("user-avatar");
    avatar.textContent = userInitials();
  }

  fragment.querySelector(".message-role").textContent = role === "assistant" ? modelLabel(message.model || state.bootstrap?.models?.primary || "gopher-ai") : "You";
  fragment.querySelector(".message-time").textContent = formatTime(message.timestamp);
  fragment.querySelector(".message-body").appendChild(renderMessageContent(message.content || ""));

  if (message.attachments?.length) {
    fragment.querySelector(".message-body").appendChild(renderAttachments(message.attachments));
  }

  const actions = fragment.querySelector(".message-actions");
  if (message.content) {
    const copyButton = document.createElement("button");
    copyButton.type = "button";
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
  state.previousModelSelection = modelsSnapshot?.primary || state.previousModelSelection || "gopher-ai";

  items.forEach(item => {
    const option = document.createElement("option");
    option.value = item.id;
    option.textContent = item.provider === "gemini" && item.status === "requires_api_key"
      ? `${item.label} · setup required`
      : item.label;
    option.selected = item.id === state.previousModelSelection;
    elements.modelSelect.appendChild(option);
  });
}

async function handleModelSelectionChange(event) {
  const selectedModel = event.target.value || "gopher-ai";

  if (isGeminiModel(selectedModel)) {
    openGeminiDialog(selectedModel);
    return;
  }

  try {
    await saveSystemSettings({ models: { primary: selectedModel } });
    state.previousModelSelection = selectedModel;
    setComposerStatus(`${modelLabel(selectedModel)} selected.`);
  } catch (error) {
    console.error(error);
    elements.modelSelect.value = state.previousModelSelection || state.bootstrap?.models?.primary || "gopher-ai";
    setComposerStatus(error.message || "Could not change the active model.");
  }
}

function openGeminiDialog(selectedModel, reason = "") {
  state.pendingGeminiModel = selectedModel;
  state.pendingGeminiReason = String(reason || "");
  elements.geminiAPIKeyInput.value = "";
  renderGeminiDialog();
  if (!elements.geminiDialog.open) {
    elements.geminiDialog.showModal();
  }
  window.setTimeout(() => {
    if (!elements.geminiUseSavedButton.hidden) {
      elements.geminiUseSavedButton.focus();
      return;
    }
    elements.geminiAPIKeyInput.focus();
  }, 40);
}

function closeGeminiDialog(saved) {
  if (elements.geminiDialog.open) {
    elements.geminiDialog.close();
  }

  if (!saved) {
    elements.modelSelect.value = state.previousModelSelection || state.bootstrap?.models?.primary || "gopher-ai";
    setComposerStatus("Gemini selection cancelled.");
  }

  state.pendingGeminiModel = null;
  state.pendingGeminiReason = "";
}

async function saveGeminiAPIKey() {
  const apiKey = elements.geminiAPIKeyInput.value.trim();
  const selectedModel = state.pendingGeminiModel || elements.modelSelect.value || "gopher-ai";

  if (!apiKey) {
    setComposerStatus("Enter a Gemini API key first.");
    elements.geminiAPIKeyInput.focus();
    return;
  }

  elements.geminiSaveButton.disabled = true;
  try {
    await saveSystemSettings({
      apiKeys: { gemini: apiKey },
      models: { primary: selectedModel },
    });
    state.previousModelSelection = selectedModel;
    closeGeminiDialog(true);
    setComposerStatus(`Gemini connected. ${modelLabel(selectedModel)} is ready.`);
  } catch (error) {
    console.error(error);
    setComposerStatus(error.message || "Could not save the Gemini API key.");
  } finally {
    elements.geminiSaveButton.disabled = false;
  }
}

function renderGeminiDialog() {
  const selectedModel = state.pendingGeminiModel || elements.modelSelect.value || geminiFallbackModel();
  const configured = Boolean(state.bootstrap?.models?.geminiConfigured);
  const reason = state.pendingGeminiReason ? ` Latest issue: ${state.pendingGeminiReason}` : "";

  if (configured) {
    elements.geminiCopy.textContent = `${modelLabel(selectedModel)} is selected. You can keep using the saved Gemini key or replace it with a new one.${reason}`;
  } else {
    elements.geminiCopy.textContent = `${modelLabel(selectedModel)} needs a Google API key before Gopher AI can use it.${reason}`;
  }

  elements.geminiUseSavedButton.hidden = !configured;
}

async function useSavedGeminiKey() {
  const selectedModel = state.pendingGeminiModel || elements.modelSelect.value || geminiFallbackModel();
  elements.geminiUseSavedButton.disabled = true;
  try {
    await saveSystemSettings({
      models: { primary: selectedModel },
    });
    state.previousModelSelection = selectedModel;
    closeGeminiDialog(true);
    setComposerStatus(`${modelLabel(selectedModel)} selected.`);
  } catch (error) {
    console.error(error);
    setComposerStatus(error.message || "Could not activate Gemini with the saved key.");
  } finally {
    elements.geminiUseSavedButton.disabled = false;
  }
}

async function resetLocalGeminiQuota() {
  elements.geminiResetQuotaButton.disabled = true;
  try {
    const quota = await api("/api/system/quota/reset", {
      method: "POST",
      body: JSON.stringify({}),
    });
    state.bootstrap = state.bootstrap || {};
    state.bootstrap.quota = quota;
    renderQuota(quota);
    setComposerStatus("Local Gemini quota was reset.");
  } catch (error) {
    console.error(error);
    setComposerStatus(error.message || "Could not reset the local Gemini quota.");
  } finally {
    elements.geminiResetQuotaButton.disabled = false;
  }
}

async function saveSystemSettings(payload) {
  const response = await api("/api/system/settings", {
    method: "POST",
    body: JSON.stringify(payload),
  });

  state.bootstrap = state.bootstrap || {};
  state.bootstrap.models = response.models;
  state.bootstrap.quota = response.quota;
  renderModels(response.models);
  renderQuota(response.quota);

  return response;
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

async function refreshQuota() {
  try {
    const quota = await api("/api/system/quota");
    state.bootstrap = state.bootstrap || {};
    state.bootstrap.quota = quota;
    renderQuota(quota);
  } catch (error) {
    console.error(error);
  }
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
    state.activeChatId = chat.id;
    state.activeChat = chat;
    renderChatList();
    renderActiveChat();
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
    state.activeChatId = response.chat?.id || chatId;
    elements.messageInput.value = "";
    state.pendingAttachments = [];
    elements.attachmentInput.value = "";
    autoResizeTextarea();
    renderPendingAttachments();
    await refreshChats();
    await refreshQuota();
    renderActiveChat();

    if (response.trainingTask?.taskId) {
      setComposerStatus(`Reply ready via ${response.modelUsed}. Training queued: ${response.trainingTask.taskId}.`);
    } else {
      setComposerStatus(`Reply ready via ${response.modelUsed}.`);
    }
  } catch (error) {
    console.error(error);
    if (shouldOpenGeminiDialog(error, elements.modelSelect.value)) {
      openGeminiDialog(geminiModelForError(elements.modelSelect.value), error.message || "");
    }
    setComposerStatus(error.message || "Could not send message.");
  } finally {
    removeTypingIndicator();
    state.sending = false;
    updateSendingState();
  }
}

async function deleteChat(chatId) {
  const chat = state.chats.find(item => item.id === chatId);
  const label = chat?.title || "this chat";
  if (!window.confirm(`Delete ${label}?`)) {
    return;
  }

  const wasActive = chatId === state.activeChatId;

  try {
    await api(`/api/chats/${chatId}`, {
      method: "DELETE",
    });
    await refreshChats();

    if (wasActive) {
      state.activeChatId = null;
      state.activeChat = null;
      if (state.chats.length > 0) {
        await openChat(state.chats[0].id);
      } else {
        renderWelcome();
      }
    } else {
      renderChatList();
    }

    setComposerStatus("Chat deleted.");
  } catch (error) {
    console.error(error);
    setComposerStatus(error.message || "Could not delete the chat.");
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
  elements.welcomeScreen.classList.add("hidden");
  elements.chatView.classList.remove("hidden");
  elements.chatEmptyState.classList.add("hidden");
  elements.messageList.classList.remove("hidden");

  const indicator = document.createElement("article");
  indicator.className = "message assistant typing";
  indicator.id = "typing-indicator";
  indicator.innerHTML = `
    <div class="avatar"><span class="assistant-blob assistant-blob-live"></span></div>
    <div class="message-card">
      <div class="message-meta">
        <span class="message-role">Gopher-AI</span>
        <span class="message-time">now</span>
      </div>
      <div class="typing-indicator">
        <span></span>
        <span></span>
        <span></span>
      </div>
    </div>
  `;
  elements.messageList.appendChild(indicator);
  state.typingIndicator = indicator;
  scrollMessagesToBottom();
}

function removeTypingIndicator() {
  state.typingIndicator?.remove();
  state.typingIndicator = null;
}

function scrollMessagesToBottom() {
  window.requestAnimationFrame(() => {
    elements.chatView.scrollTop = elements.chatView.scrollHeight;
  });
}

function autoResizeTextarea() {
  elements.messageInput.style.height = "auto";
  elements.messageInput.style.height = `${Math.min(elements.messageInput.scrollHeight, 192)}px`;
}

function setComposerStatus(message) {
  elements.composerStatus.textContent = message;
}

function startAutoRefresh() {
  window.clearInterval(state.autoRefreshTimer);
  state.autoRefreshTimer = window.setInterval(backgroundRefresh, 8000);
}

async function backgroundRefresh() {
  if (document.hidden || state.sending || elements.trainingDialog.open || elements.geminiDialog.open) {
    return;
  }

  try {
    const previousSignature = chatSignature(state.activeChat);
    await refreshChats(elements.chatSearch.value.trim());

    if (!state.activeChatId) {
      if (state.chats.length === 0) {
        renderWelcome();
      }
      return;
    }

    const chat = await api(`/api/chats/${state.activeChatId}`);
    const nextSignature = chatSignature(chat);
    if (nextSignature !== previousSignature) {
      state.activeChat = chat;
      renderActiveChat();
    }
  } catch (error) {
    console.error(error);
  }
}

function chatSignature(chat) {
  if (!chat) {
    return "";
  }
  const messages = chat.messages || [];
  const last = messages[messages.length - 1];
  return `${chat.id}:${chat.updatedAt || ""}:${messages.length}:${last?.id || ""}`;
}

function handleViewportChange() {
  if (isNarrowLayout()) {
    if (elements.appShell.classList.contains("sidebar-collapsed")) {
      elements.appShell.classList.remove("sidebar-collapsed");
    }
    if (!state.sidebarOpen) {
      setSidebarOpen(false);
      return;
    }
  }
  applySidebarState();
}

function setSidebarOpen(open) {
  state.sidebarOpen = open;
  applySidebarState();
}

function applySidebarState() {
  const narrow = isNarrowLayout();

  if (narrow) {
    elements.appShell.classList.toggle("sidebar-open", state.sidebarOpen);
    elements.appShell.classList.remove("sidebar-collapsed");
    elements.sidebarOverlay.classList.toggle("visible", state.sidebarOpen);
    return;
  }

  elements.appShell.classList.remove("sidebar-open");
  elements.sidebarOverlay.classList.remove("visible");
  elements.appShell.classList.toggle("sidebar-collapsed", !state.sidebarOpen);
}

function isNarrowLayout() {
  return window.innerWidth <= 960;
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

function shouldOpenGeminiDialog(error, selectedModel) {
  if (isGeminiModel(selectedModel)) {
    return true;
  }

  const message = String(error?.message || "").toLowerCase();
  return [
    "gemini",
    "api key",
    "quota",
    "rate limit",
    "rate limited",
    "permission",
    "unauthorized",
    "forbidden",
    "429",
  ].some(part => message.includes(part));
}

function geminiModelForError(selectedModel) {
  if (isGeminiModel(selectedModel)) {
    return selectedModel;
  }
  return geminiFallbackModel();
}

function geminiFallbackModel() {
  return state.bootstrap?.models?.fallback || "gemini-3.1-pro-preview";
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

function isGeminiModel(value) {
  return String(value || "").startsWith("gemini-");
}

function modelLabel(modelID) {
  const models = state.bootstrap?.models?.availableModels || [];
  return models.find(item => item.id === modelID)?.label || modelID;
}

function userInitials() {
  const username = state.bootstrap?.user?.username || "You";
  return username
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map(item => item[0].toUpperCase())
    .join("");
}
