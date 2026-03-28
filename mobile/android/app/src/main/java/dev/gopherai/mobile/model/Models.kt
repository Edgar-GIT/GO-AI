package dev.gopherai.mobile.model

import kotlinx.serialization.Serializable

@Serializable
data class BootstrapResponse(
    val user: UserProfile = UserProfile(),
    val models: ModelsSnapshot = ModelsSnapshot(),
    val quota: QuotaSnapshot = QuotaSnapshot(),
    val chats: List<ChatSummary> = emptyList()
)

@Serializable
data class UserProfile(
    val username: String = "Edgar",
    val theme: String = "sleepy-dark-blue-aquarium"
)

@Serializable
data class ModelsSnapshot(
    val primary: String = "gopher-ai",
    val fallback: String = "",
    val fallbackSecondary: String = "",
    val local: LocalStatus = LocalStatus(),
    val geminiConfigured: Boolean = false,
    val availableModels: List<ModelDescriptor> = emptyList()
)

@Serializable
data class LocalStatus(
    val enabled: Boolean = true,
    val configured: Boolean = false,
    val reachable: Boolean = false,
    val autoStart: Boolean = false,
    val serverUrl: String = "",
    val binaryPath: String = "",
    val modelPath: String = "",
    val modelAlias: String = "local-llama",
    val contextSize: Int = 4096,
    val maxTokens: Int = 512,
    val gpuLayers: Int = -1,
    val flashAttention: Boolean = true,
    val status: String = "unknown"
)

@Serializable
data class ModelDescriptor(
    val id: String = "",
    val label: String = "",
    val provider: String = "",
    val tier: String = "",
    val lifecycle: String = "",
    val status: String = "",
    val supportsThinking: Boolean = false
)

@Serializable
data class QuotaSnapshot(
    val gemini: GeminiQuota = GeminiQuota()
)

@Serializable
data class GeminiQuota(
    val totalTokensUsedToday: Int = 0,
    val totalTokensLimit: Int = 0,
    val requestCount: Int = 0,
    val requestLimit: Int = 0,
    val resetTime: String = ""
)

@Serializable
data class ChatListResponse(
    val items: List<ChatSummary> = emptyList(),
    val limit: Int = 20,
    val offset: Int = 0
)

@Serializable
data class ChatSummary(
    val id: String = "",
    val title: String = "New Chat",
    val updatedAt: String = "",
    val modelUsed: String = "",
    val messageCount: Int = 0,
    val lastMessagePreview: String = ""
)

@Serializable
data class Chat(
    val id: String = "",
    val title: String = "New Chat",
    val createdAt: String = "",
    val updatedAt: String = "",
    val modelUsed: String = "",
    val messages: List<Message> = emptyList(),
    val metadata: ChatMetadata = ChatMetadata()
)

@Serializable
data class ChatMetadata(
    val totalTokensUsed: Int = 0,
    val messageCount: Int = 0,
    val attachmentCount: Int = 0
)

@Serializable
data class Message(
    val id: String = "",
    val role: String = "assistant",
    val content: String = "",
    val model: String = "",
    val latency: Long = 0,
    val tokensUsed: Int = 0,
    val timestamp: String = "",
    val attachments: List<AttachmentRef> = emptyList()
)

@Serializable
data class AttachmentRef(
    val id: String = "",
    val filename: String = "",
    val size: Long = 0,
    val mimeType: String = "",
    val hash: String = ""
)

@Serializable
data class CreateChatRequest(
    val title: String = "",
    val model: String = "gopher-ai"
)

@Serializable
data class SendMessageRequest(
    val content: String,
    val attachmentIds: List<String> = emptyList(),
    val forceModel: String? = null
)

@Serializable
data class SendMessageResponse(
    val chat: Chat = Chat(),
    val modelUsed: String = "",
    val fallbackUsed: Boolean = false,
    val tokensUsed: Int = 0,
    val latency: Long = 0
)

@Serializable
data class PendingQueueFile(
    val pendingMessages: List<PendingMessage> = emptyList()
)

@Serializable
data class PendingMessage(
    val tempId: String,
    val chatId: String,
    val content: String,
    val forceModel: String? = null,
    val queuedAt: String,
    val status: String = "pending"
)
