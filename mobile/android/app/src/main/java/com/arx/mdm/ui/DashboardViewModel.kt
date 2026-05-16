package com.arx.mdm.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.arx.mdm.AgentService
import com.arx.mdm.network.ArxServerPing
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class DashboardViewModel(
    application: Application,
) : AndroidViewModel(application) {

    private val _uiState = MutableStateFlow(
        DashboardUiState.snapshot(application, C2ConnectionStatus.OFFLINE, null),
    )
    val uiState: StateFlow<DashboardUiState> = _uiState.asStateFlow()

    private val _lastPingSummary = MutableStateFlow<String?>(null)
    val lastPingSummary: StateFlow<String?> = _lastPingSummary.asStateFlow()

    private var boundService: AgentService? = null
    private var collectJob: Job? = null

    fun attachAgentService(service: AgentService) {
        boundService = service
        collectJob?.cancel()
        collectJob = viewModelScope.launch {
            service.dashboardState.collect { _uiState.value = it }
        }
    }

    fun detachAgentService() {
        collectJob?.cancel()
        collectJob = null
        boundService = null
    }

    fun onForceReconnectClicked() {
        boundService?.forceReconnectWebSocket()
    }

    fun onSyncTelemetryClicked() {
        boundService?.requestImmediateTelemetryPush()
    }

    fun onPingServerClicked() {
        viewModelScope.launch {
            val result = withContext(Dispatchers.IO) {
                ArxServerPing.ping(getApplication())
            }
            _lastPingSummary.value = result.summarize()
        }
    }
}
