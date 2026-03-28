package dev.gopherai.mobile.data

import android.content.Context
import dev.gopherai.mobile.model.PendingMessage
import dev.gopherai.mobile.model.PendingQueueFile
import dev.gopherai.mobile.network.GopherApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import java.io.File
import java.time.Instant

data class QueueSyncSummary(
    val sentCount: Int,
    val remainingCount: Int,
    val touchedChatIds: Set<String>
)

class QueueStore(
    context: Context,
    private val json: Json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        prettyPrint = true
    }
) {
    private val queueFile = File(context.filesDir, "queue.json")

    suspend fun load(): PendingQueueFile = withContext(Dispatchers.IO) {
        if (!queueFile.exists()) {
            return@withContext PendingQueueFile()
        }
        json.decodeFromString(queueFile.readText())
    }

    suspend fun enqueue(chatId: String, content: String, forceModel: String?) {
        val state = load()
        val item = PendingMessage(
            tempId = "tmp_${System.currentTimeMillis()}",
            chatId = chatId,
            content = content,
            forceModel = forceModel,
            queuedAt = Instant.now().toString()
        )
        save(state.copy(pendingMessages = state.pendingMessages + item))
    }

    suspend fun sync(api: GopherApi, baseUrl: String): QueueSyncSummary {
        val state = load()
        if (state.pendingMessages.isEmpty()) {
            return QueueSyncSummary(sentCount = 0, remainingCount = 0, touchedChatIds = emptySet())
        }

        val remaining = mutableListOf<PendingMessage>()
        val touchedChatIds = linkedSetOf<String>()
        var sentCount = 0

        state.pendingMessages.forEach { item ->
            try {
                api.sendMessage(
                    baseUrl = baseUrl,
                    chatId = item.chatId,
                    content = item.content,
                    forceModel = item.forceModel
                )
                sentCount += 1
                touchedChatIds += item.chatId
            } catch (_: Exception) {
                remaining += item
            }
        }

        save(PendingQueueFile(pendingMessages = remaining))
        return QueueSyncSummary(sentCount = sentCount, remainingCount = remaining.size, touchedChatIds = touchedChatIds)
    }

    private suspend fun save(state: PendingQueueFile) = withContext(Dispatchers.IO) {
        queueFile.parentFile?.mkdirs()
        queueFile.writeText(json.encodeToString(PendingQueueFile.serializer(), state))
    }
}
