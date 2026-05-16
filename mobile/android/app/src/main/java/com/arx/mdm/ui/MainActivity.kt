package com.arx.mdm.ui

import android.content.ComponentName
import android.content.Context
import android.content.ServiceConnection
import android.os.Bundle
import android.os.IBinder
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.ui.Modifier
import com.arx.mdm.AgentService

class MainActivity : ComponentActivity() {

    private val viewModel: DashboardViewModel by viewModels()
    private var serviceConnection: ServiceConnection? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            MaterialTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    DashboardRoute(viewModel = viewModel)
                }
            }
        }
    }

    override fun onStart() {
        super.onStart()
        AgentService.startOrRestart(this)
        val conn = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
                val svc = (binder as? AgentService.LocalBinder)?.getService() ?: return
                viewModel.attachAgentService(svc)
            }

            override fun onServiceDisconnected(name: ComponentName?) {
                viewModel.detachAgentService()
            }
        }
        bindService(
            android.content.Intent(this, AgentService::class.java),
            conn,
            Context.BIND_AUTO_CREATE,
        )
        serviceConnection = conn
    }

    override fun onStop() {
        serviceConnection?.let { unbindService(it) }
        serviceConnection = null
        viewModel.detachAgentService()
        super.onStop()
    }
}
