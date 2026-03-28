package dev.gopherai.mobile.ui

import android.app.Application
import android.content.Context
import android.net.nsd.NsdManager
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import dev.gopherai.mobile.data.QueueStore
import dev.gopherai.mobile.model.BootstrapResponse
import dev.gopherai.mobile.model.Chat
import dev.gopherai.mobile.model.ChatSummary
import dev.gopherai.mobile.model.ModelDescriptor
import dev.gopherai.mobile.network.DiscoveredServer
import dev.gopherai.mobile.network.GopherApi
import dev.gopherai.mobile.network.MdnsDiscovery
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

data class MobileUiState(
    val username: String = "Edgar",
    val serverUrl: String = "http://127.0.0.1:8080",
    val chats: List<ChatSummary> = emptyList(),
    val activeChat: Chat? = null,
    val selectedModel: String = "gopher-ai",
    val availableModels: List<ModelDescriptor> = emptyList(),
    val draftMessage: String = "",
    val statusMessage: String = "Ready.",
    val discoveredServers: List<DiscoveredServer> = emptyList(),
    val pendingQueueCount: Int = 0,
    val isLoading: Boolean = false,
    val isSending: Boolean = false
)

class MainViewModel(application: Application) : AndroidViewModel(application) {
    private val api = GopherApi()
    private val queueStore = QueueStore(application)
    private val discovery = MdnsDiscovery(application)
    private val prefs = application.getSharedPreferences("gopher_ai_mobile", Context.MODE_PRIVATE)

    private var discoveryListener: NsdManager.DiscoveryListener? = null

    var uiState by mutableStateOf(
        MobileUiState(
            serverUrl = prefs.getString("server_url", "http://127.0.0.1:8080").orEmpty().ifBlank {
                "http://127.0.0.1:8080"
            }
        )
    )
        private set

    init {
        refresh()
    }

    override fun onCleared() {
        discovery.stop(discoveryListener)
        super.onCleared()
    }

    fun updateServerUrl(value: String) {
        uiState = uiState.copy(serverUrl = value)
    }

    fun updateDraftMessage(value: String) {
        uiState = uiState.copy(draftMessage = value)
    }

    fun selectModel(modelId: String) {
        uiState = uiState.copy(selectedModel = modelId)
    }

    fun connectToServer() {
        prefs.edit().putString("server_url", uiState.serverUrl).apply()
        refresh()
    }

    fun refresh() {
        viewModelScope.launch {
            uiState = uiState.copy(isLoading = true, statusMessage = "Connecting to ${uiState.serverUrl}...")
            try {
                val bootstrap = api.bootstrap(uiState.serverUrl)
                applyBootstrap(bootstrap)
                syncQueue()
                if (uiState.activeChat == null && uiState.chats.isNotEmpty()) {
                    openChat(uiState.chats.first().id)
                } else {
                    uiState = uiState.copy(isLoading = false, statusMessage = "Connected.")
                }
            } catch (error: Exception) {
                uiState = uiState.copy(
                    isLoading = false,
                    statusMessage = error.message ?: "Could not connect to the Gopher AI backend."
                )
            }
        }
    }

    fun createChat() {
        viewModelScope.launch {
            uiState = uiState.copy(isLoading = true, statusMessage = "Creating chat...")
            try {
                val chat = api.createChat(uiState.serverUrl, model = uiState.selectedModel)
                uiState = uiState.copy(
                    chats = listOf(chat.toSummary()) + uiState.chats.filterNot { it.id == chat.id },
                    activeChat = chat,
                    isLoading = false,
                    statusMessage = "Chat created."
                )
            } catch (error: Exception) {
                uiState = uiState.copy(isLoading = false, statusMessage = error.message ?: "Could not create chat.")
            }
        }
    }

    fun openChat(chatId: String) {
        viewModelScope.launch {
            uiState = uiState.copy(isLoading = true, statusMessage = "Loading chat...")
            try {
                val chat = api.getChat(uiState.serverUrl, chatId)
                uiState = uiState.copy(activeChat = chat, isLoading = false, statusMessage = "Chat ready.")
            } catch (error: Exception) {
                uiState = uiState.copy(isLoading = false, statusMessage = error.message ?: "Could not load chat.")
            }
        }
    }

    fun sendMessage() {
        val draft = uiState.draftMessage.trim()
        if (draft.isBlank()) {
            uiState = uiState.copy(statusMessage = "Write a message first.")
            return
        }

        viewModelScope.launch {
            var activeChat = uiState.activeChat
            if (activeChat == null) {
                try {
                    activeChat = api.createChat(uiState.serverUrl, model = uiState.selectedModel)
                    uiState = uiState.copy(
                        chats = listOf(activeChat.toSummary()) + uiState.chats,
                        activeChat = activeChat
                    )
                } catch (error: Exception) {
                    uiState = uiState.copy(statusMessage = error.message ?: "Could not create a chat.")
                    return@launch
                }
            }

            val chatId = activeChat.id
            uiState = uiState.copy(isSending = true, statusMessage = "Sending...")

            try {
                val response = api.sendMessage(
                    baseUrl = uiState.serverUrl,
                    chatId = chatId,
                    content = draft,
                    forceModel = uiState.selectedModel
                )
                uiState = uiState.copy(
                    activeChat = response.chat,
                    chats = listOf(response.chat.toSummary()) + uiState.chats.filterNot { it.id == response.chat.id },
                    draftMessage = "",
                    isSending = false,
                    statusMessage = "Reply ready via ${response.modelUsed}.",
                    pendingQueueCount = queueStore.load().pendingMessages.size
                )
            } catch (error: Exception) {
                queueStore.enqueue(chatId = chatId, content = draft, forceModel = uiState.selectedModel)
                uiState = uiState.copy(
                    draftMessage = "",
                    isSending = false,
                    pendingQueueCount = queueStore.load().pendingMessages.size,
                    statusMessage = "Offline or unreachable. Message queued for sync."
                )
            }
        }
    }

    fun discoverServers() {
        discovery.stop(discoveryListener)
        uiState = uiState.copy(discoveredServers = emptyList(), statusMessage = "Starting LAN discovery...")
        discoveryListener = discovery.discover(
            onServerFound = { server ->
                uiState = uiState.copy(
                    discoveredServers = (uiState.discoveredServers + server).distinctBy { it.endpoint }
                )
            },
            onStatus = { status ->
                uiState = uiState.copy(statusMessage = status)
            }
        )

        viewModelScope.launch {
            delay(5_000)
            discovery.stop(discoveryListener)
        }
    }

    fun useDiscoveredServer(server: DiscoveredServer) {
        updateServerUrl(server.endpoint)
        connectToServer()
    }

    private fun applyBootstrap(bootstrap: BootstrapResponse) {
        uiState = uiState.copy(
            username = bootstrap.user.username,
            chats = bootstrap.chats,
            availableModels = bootstrap.models.availableModels,
            selectedModel = bootstrap.models.primary.ifBlank { "gopher-ai" },
            pendingQueueCount = uiState.pendingQueueCount,
            isLoading = false,
            statusMessage = "Connected to ${uiState.serverUrl}."
        )
    }

    private suspend fun syncQueue() {
        val summary = queueStore.sync(api, uiState.serverUrl)
        if (summary.sentCount > 0) {
            uiState = uiState.copy(statusMessage = "Synced ${summary.sentCount} queued messages.")
        }
        uiState = uiState.copy(pendingQueueCount = summary.remainingCount)
        if (uiState.activeChat?.id in summary.touchedChatIds) {
            uiState.activeChat?.id?.let(::openChat)
        }
    }
}

private fun Chat.toSummary(): ChatSummary = ChatSummary(
    id = id,
    title = title,
    updatedAt = updatedAt,
    modelUsed = modelUsed,
    messageCount = metadata.messageCount,
    lastMessagePreview = messages.lastOrNull()?.content.orEmpty()
)
