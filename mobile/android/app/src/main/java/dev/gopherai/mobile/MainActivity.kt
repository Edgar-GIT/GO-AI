package dev.gopherai.mobile

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.gopherai.mobile.model.Chat
import dev.gopherai.mobile.model.ChatSummary
import dev.gopherai.mobile.model.Message
import dev.gopherai.mobile.ui.MainViewModel
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            GopherAiTheme {
                val viewModel: MainViewModel = viewModel()
                GopherAiApp(viewModel)
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun GopherAiApp(viewModel: MainViewModel) {
    val state = viewModel.uiState
    val drawerState = androidx.compose.material3.rememberDrawerState(initialValue = androidx.compose.material3.DrawerValue.Closed)
    val scope = rememberCoroutineScope()

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(
                brush = Brush.linearGradient(
                    listOf(
                        Color(0xFF000A2E),
                        Color(0xFF001A5C),
                        Color(0xFF003D99),
                        Color(0xFF001A5C),
                        Color(0xFF000A2E)
                    )
                )
            )
    ) {
        ModalNavigationDrawer(
            drawerState = drawerState,
            drawerContent = {
                DrawerContent(
                    state = state,
                    onServerUrlChange = viewModel::updateServerUrl,
                    onConnect = viewModel::connectToServer,
                    onDiscover = viewModel::discoverServers,
                    onPickServer = viewModel::useDiscoveredServer,
                    onSelectChat = {
                        scope.launch { drawerState.close() }
                        viewModel.openChat(it.id)
                    }
                )
            }
        ) {
            Scaffold(
                containerColor = Color.Transparent,
                topBar = {
                    TopAppBar(
                        title = {
                            Column {
                                Text("Gopher AI", fontWeight = FontWeight.Bold)
                                Text(
                                    text = state.statusMessage,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.secondary
                                )
                            }
                        },
                        navigationIcon = {
                            TextButton(onClick = { scope.launch { drawerState.open() } }) {
                                Text("Menu")
                            }
                        },
                        actions = {
                            TextButton(onClick = viewModel::refresh) { Text("Refresh") }
                        }
                    )
                },
                bottomBar = {
                    Composer(
                        state = state,
                        onDraftChange = viewModel::updateDraftMessage,
                        onModelChange = viewModel::selectModel,
                        onSend = viewModel::sendMessage
                    )
                }
            ) { padding ->
                Surface(
                    modifier = Modifier
                        .padding(padding)
                        .fillMaxSize()
                        .padding(horizontal = 12.dp, vertical = 6.dp),
                    shape = RoundedCornerShape(24.dp),
                    color = Color(0xAA04164B)
                ) {
                    if (state.activeChat == null) {
                        WelcomePanel(
                            username = state.username,
                            onCreateChat = viewModel::createChat
                        )
                    } else {
                        ChatPanel(chat = state.activeChat)
                    }
                }
            }
        }
    }
}

@Composable
private fun DrawerContent(
    state: dev.gopherai.mobile.ui.MobileUiState,
    onServerUrlChange: (String) -> Unit,
    onConnect: () -> Unit,
    onDiscover: () -> Unit,
    onPickServer: (dev.gopherai.mobile.network.DiscoveredServer) -> Unit,
    onSelectChat: (ChatSummary) -> Unit
) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp)
    ) {
        Card(colors = CardDefaults.cardColors(containerColor = Color(0xCC07235C))) {
            Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("Server", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                OutlinedTextField(
                    value = state.serverUrl,
                    onValueChange = onServerUrlChange,
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("PC server URL") },
                    singleLine = true
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(onClick = onConnect) { Text("Connect") }
                    OutlinedButton(onClick = onDiscover) { Text("Discover LAN") }
                }
                if (state.discoveredServers.isNotEmpty()) {
                    Text("Discovered", style = MaterialTheme.typography.labelLarge)
                    state.discoveredServers.forEach { server ->
                        NavigationDrawerItem(
                            label = { Text(server.endpoint, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                            selected = state.serverUrl == server.endpoint,
                            onClick = { onPickServer(server) }
                        )
                    }
                }
                Text("Queued offline: ${state.pendingQueueCount}", color = MaterialTheme.colorScheme.secondary)
            }
        }

        Card(colors = CardDefaults.cardColors(containerColor = Color(0xAA02143F))) {
            Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Chats", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                if (state.chats.isEmpty()) {
                    Text("No chats yet.")
                } else {
                    state.chats.forEach { chat ->
                        NavigationDrawerItem(
                            label = {
                                Column {
                                    Text(chat.title, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    Text(
                                        chat.lastMessagePreview,
                                        style = MaterialTheme.typography.bodySmall,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis
                                    )
                                }
                            },
                            selected = state.activeChat?.id == chat.id,
                            onClick = { onSelectChat(chat) }
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun WelcomePanel(username: String, onCreateChat: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        Box(
            modifier = Modifier
                .size(120.dp)
                .background(
                    color = Color(0x3327F7FF),
                    shape = RoundedCornerShape(38.dp)
                )
        )
        Spacer(modifier = Modifier.height(16.dp))
        Text("Welcome to Gopher AI, $username", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
        Spacer(modifier = Modifier.height(8.dp))
        Text("Connect to your PC and start a local-first conversation.")
        Spacer(modifier = Modifier.height(18.dp))
        Button(onClick = onCreateChat) {
            Text("Create first chat")
        }
    }
}

@Composable
private fun ChatPanel(chat: Chat) {
    LazyColumn(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)
    ) {
        item {
            Text(chat.title, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
            Text("${chat.messages.size} messages · ${chat.modelUsed}", color = MaterialTheme.colorScheme.secondary)
        }
        items(chat.messages, key = { it.id }) { message ->
            MessageBubble(message = message)
        }
        item {
            Spacer(modifier = Modifier.height(88.dp))
        }
    }
}

@Composable
private fun MessageBubble(message: Message) {
    val isUser = message.role == "user"
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = if (isUser) Arrangement.End else Arrangement.Start
    ) {
        Card(
            modifier = Modifier.fillMaxWidth(if (isUser) 0.84f else 0.92f),
            shape = RoundedCornerShape(22.dp),
            colors = CardDefaults.cardColors(
                containerColor = if (isUser) Color(0xCC00133E) else Color(0xCC0A2A72)
            )
        ) {
            Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    if (isUser) "You" else (message.model.ifBlank { "Gopher AI" }),
                    color = MaterialTheme.colorScheme.secondary,
                    fontWeight = FontWeight.Bold
                )
                Text(message.content.ifBlank { "(empty)" })
                if (message.attachments.isNotEmpty()) {
                    Text(
                        message.attachments.joinToString { it.filename },
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.secondary
                    )
                }
            }
        }
    }
}

@Composable
private fun Composer(
    state: dev.gopherai.mobile.ui.MobileUiState,
    onDraftChange: (String) -> Unit,
    onModelChange: (String) -> Unit,
    onSend: () -> Unit
) {
    var expanded by remember { mutableStateOf(false) }

    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(12.dp),
        colors = CardDefaults.cardColors(containerColor = Color(0xE603183F)),
        shape = RoundedCornerShape(24.dp)
    ) {
        Column(modifier = Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Box {
                OutlinedButton(onClick = { expanded = true }) {
                    Text(
                        state.availableModels.firstOrNull { it.id == state.selectedModel }?.label
                            ?: state.selectedModel.ifBlank { "Select model" }
                    )
                }
                DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
                    state.availableModels.forEach { model ->
                        DropdownMenuItem(
                            text = { Text("${model.label} · ${model.status}") },
                            onClick = {
                                expanded = false
                                onModelChange(model.id)
                            }
                        )
                    }
                }
            }

            OutlinedTextField(
                value = state.draftMessage,
                onValueChange = onDraftChange,
                modifier = Modifier.fillMaxWidth(),
                label = { Text("Message") },
                placeholder = { Text("Ask your PC Gopher AI something...") },
                minLines = 2,
                maxLines = 5
            )

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text(
                    "Mobile attachments are the next pass.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.secondary
                )
                Button(onClick = onSend, enabled = !state.isSending) {
                    Text(if (state.isSending) "Sending..." else "Send")
                }
            }
        }
    }
}

@Composable
private fun GopherAiTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            primary = Color(0xFF00CCFF),
            secondary = Color(0xFF7FFEFF),
            tertiary = Color(0xFF0066FF),
            background = Color(0xFF000A2E),
            surface = Color(0xFF04164B),
            onPrimary = Color.White,
            onSecondary = Color(0xFFE8FFFF),
            onBackground = Color(0xFFE8FFFF),
            onSurface = Color(0xFFE8FFFF)
        ),
        content = content
    )
}
