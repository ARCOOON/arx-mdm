package com.arx.mdm.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import java.text.DateFormat
import java.util.Date

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun DashboardRoute(viewModel: DashboardViewModel) {
    val ui by viewModel.uiState.collectAsStateWithLifecycle()
    val pingLine by viewModel.lastPingSummary.collectAsStateWithLifecycle()
    DashboardScreen(
        state = ui,
        lastPingSummary = pingLine,
        onForceReconnect = { viewModel.onForceReconnectClicked() },
        onSyncTelemetry = { viewModel.onSyncTelemetryClicked() },
        onPingServer = { viewModel.onPingServerClicked() },
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun DashboardScreen(
    state: DashboardUiState,
    lastPingSummary: String?,
    onForceReconnect: () -> Unit,
    onSyncTelemetry: () -> Unit,
    onPingServer: () -> Unit,
) {
    val scroll = rememberScrollState()
    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(scroll)
            .padding(horizontal = 20.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = "ARX MDM",
            style = MaterialTheme.typography.headlineMedium,
            fontWeight = FontWeight.SemiBold,
        )
        Text(
            text = "Device diagnostics and connection health",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        Card(elevation = CardDefaults.cardElevation(defaultElevation = 2.dp)) {
            Column(
                modifier = Modifier.padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                SectionTitle("Server & connection")
                KeyValueRow("Server URL", state.serverUrlDisplay)
                KeyValueRow("WebSocket", state.connectionLabel())
                KeyValueRow("Enrollment", state.enrollmentLabel())
                KeyValueRow(
                    "Last telemetry sync",
                    formatSyncTime(state.lastTelemetrySyncEpochMillis),
                )
            }
        }

        Card(elevation = CardDefaults.cardElevation(defaultElevation = 2.dp)) {
            Column(
                modifier = Modifier.padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                SectionTitle("MDM & certificates")
                KeyValueRow(
                    "Device owner",
                    if (state.deviceOwnerActive) "Active" else "Inactive",
                )
                KeyValueRow(
                    "mTLS client material (app files)",
                    yesNo(state.mtlsClientCertPresent),
                )
                KeyValueRow(
                    "Root CA in secure storage",
                    yesNo(state.rootCaStored),
                )
            }
        }

        Card(elevation = CardDefaults.cardElevation(defaultElevation = 2.dp)) {
            Column(
                modifier = Modifier.padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                SectionTitle("Diagnostics")
                FlowRow(
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Button(
                        onClick = onForceReconnect,
                        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
                    ) {
                        Text("Force reconnect")
                    }
                    Button(
                        onClick = onSyncTelemetry,
                        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
                    ) {
                        Text("Sync telemetry now")
                    }
                    Button(
                        onClick = onPingServer,
                        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
                    ) {
                        Text("Ping server")
                    }
                }
                if (lastPingSummary != null) {
                    HorizontalDivider()
                    Text(
                        text = "Last ping: $lastPingSummary",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }

        Text(
            text = state.goBindVersionLine,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.outline,
        )
    }
}

@Composable
private fun SectionTitle(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.titleMedium,
        fontWeight = FontWeight.Medium,
    )
}

@Composable
private fun KeyValueRow(label: String, value: String) {
    Column(modifier = Modifier.fillMaxWidth()) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = value,
            style = MaterialTheme.typography.bodyLarge,
        )
    }
}

private fun yesNo(ok: Boolean): String = if (ok) "Present" else "Missing"

private fun formatSyncTime(epochMillis: Long?): String {
    if (epochMillis == null || epochMillis <= 0L) return "—"
    return DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.MEDIUM).format(Date(epochMillis))
}
