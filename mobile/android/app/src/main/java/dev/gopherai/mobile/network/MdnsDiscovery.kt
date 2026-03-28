package dev.gopherai.mobile.network

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import java.util.Locale

data class DiscoveredServer(
    val name: String,
    val endpoint: String
)

class MdnsDiscovery(private val context: Context) {
    private val nsdManager: NsdManager by lazy {
        context.getSystemService(Context.NSD_SERVICE) as NsdManager
    }

    fun discover(
        onServerFound: (DiscoveredServer) -> Unit,
        onStatus: (String) -> Unit
    ): NsdManager.DiscoveryListener {
        val listener = object : NsdManager.DiscoveryListener {
            override fun onDiscoveryStarted(regType: String) {
                onStatus("Discovering Gopher AI servers on the LAN...")
            }

            override fun onServiceFound(service: NsdServiceInfo) {
                val type = service.serviceType?.lowercase(Locale.US).orEmpty()
                if (!type.contains("_gopher._tcp")) {
                    return
                }
                resolve(service, onServerFound, onStatus)
            }

            override fun onServiceLost(service: NsdServiceInfo) {
                onStatus("Server left the LAN: ${service.serviceName}")
            }

            override fun onDiscoveryStopped(serviceType: String) {
                onStatus("LAN discovery finished.")
            }

            override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) {
                onStatus("LAN discovery failed: $errorCode")
                nsdManager.stopServiceDiscovery(this)
            }

            override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) {
                onStatus("Could not stop LAN discovery cleanly: $errorCode")
                nsdManager.stopServiceDiscovery(this)
            }
        }

        nsdManager.discoverServices("_gopher._tcp.", NsdManager.PROTOCOL_DNS_SD, listener)
        return listener
    }

    fun stop(listener: NsdManager.DiscoveryListener?) {
        if (listener == null) {
            return
        }
        runCatching {
            nsdManager.stopServiceDiscovery(listener)
        }
    }

    @Suppress("DEPRECATION")
    private fun resolve(
        service: NsdServiceInfo,
        onServerFound: (DiscoveredServer) -> Unit,
        onStatus: (String) -> Unit
    ) {
        nsdManager.resolveService(service, object : NsdManager.ResolveListener {
            override fun onResolveFailed(serviceInfo: NsdServiceInfo, errorCode: Int) {
                onStatus("Could not resolve ${serviceInfo.serviceName}: $errorCode")
            }

            override fun onServiceResolved(serviceInfo: NsdServiceInfo) {
                val host = serviceInfo.host?.hostAddress ?: return
                val endpoint = "http://$host:${serviceInfo.port}"
                onServerFound(DiscoveredServer(serviceInfo.serviceName ?: "Gopher AI", endpoint))
                onStatus("Found ${serviceInfo.serviceName} at $endpoint")
            }
        })
    }
}
