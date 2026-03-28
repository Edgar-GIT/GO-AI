package dev.gopherai.mobile.network

import dev.gopherai.mobile.model.BootstrapResponse
import dev.gopherai.mobile.model.Chat
import dev.gopherai.mobile.model.ChatListResponse
import dev.gopherai.mobile.model.CreateChatRequest
import dev.gopherai.mobile.model.SendMessageRequest
import dev.gopherai.mobile.model.SendMessageResponse
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.net.URLEncoder
import java.util.concurrent.TimeUnit

class GopherApi(
    private val json: Json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
    }
) {
    private val httpClient = OkHttpClient.Builder()
        .connectTimeout(20, TimeUnit.SECONDS)
        .readTimeout(60, TimeUnit.SECONDS)
        .writeTimeout(60, TimeUnit.SECONDS)
        .build()

    suspend fun bootstrap(baseUrl: String): BootstrapResponse =
        get(baseUrl, "/api/app/bootstrap")

    suspend fun listChats(baseUrl: String, search: String = ""): ChatListResponse {
        val query = if (search.isBlank()) "" else "?search=${URLEncoder.encode(search, "UTF-8")}"
        return get(baseUrl, "/api/chats$query")
    }

    suspend fun getChat(baseUrl: String, chatId: String): Chat =
        get(baseUrl, "/api/chats/$chatId")

    suspend fun createChat(baseUrl: String, title: String = "", model: String): Chat =
        post(baseUrl, "/api/chats", CreateChatRequest(title = title, model = model))

    suspend fun sendMessage(
        baseUrl: String,
        chatId: String,
        content: String,
        forceModel: String?,
        attachmentIds: List<String> = emptyList()
    ): SendMessageResponse = post(
        baseUrl,
        "/api/chats/$chatId/messages",
        SendMessageRequest(content = content, forceModel = forceModel, attachmentIds = attachmentIds)
    )

    private suspend inline fun <reified T> get(baseUrl: String, path: String): T = request(
        Request.Builder()
            .url("${normalizeBaseUrl(baseUrl)}$path")
            .get()
            .build()
    )

    private suspend inline fun <reified Req : Any, reified Res> post(baseUrl: String, path: String, payload: Req): Res {
        val body = json.encodeToString(payload).toRequestBody("application/json".toMediaType())
        return request(
            Request.Builder()
                .url("${normalizeBaseUrl(baseUrl)}$path")
                .post(body)
                .build()
        )
    }

    private suspend inline fun <reified T> request(request: Request): T = withContext(Dispatchers.IO) {
        httpClient.newCall(request).execute().use { response ->
            val body = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                val error = runCatching { json.decodeFromString(ApiError.serializer(), body).error }
                    .getOrElse { body.ifBlank { "HTTP ${response.code}" } }
                throw IllegalStateException(error)
            }
            json.decodeFromString(body)
        }
    }

    private fun normalizeBaseUrl(baseUrl: String): String =
        baseUrl.trim().trimEnd('/').ifBlank { "http://127.0.0.1:8080" }
}

@Serializable
private data class ApiError(
    val error: String = "Unknown request failure"
)
