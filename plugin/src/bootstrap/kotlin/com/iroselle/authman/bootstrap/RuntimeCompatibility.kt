package com.iroselle.authman.bootstrap

import com.iroselle.authman.spi.AUTHMAN_RUNTIME_API_VERSION
import com.iroselle.authman.spi.AUTHMAN_RUNTIME_CONTRACT

/**
 * API 1 is accepted only so the stable bootstrap can replace the first
 * production loader without requiring a coordinated Runtime update. API 2 is
 * the frozen contract and must remain the contract for future Runtime builds.
 */
object RuntimeCompatibility {
    const val MIGRATION_API_VERSION = 1

    fun requireCompatible(apiVersion: Int, contract: String?) {
        require(apiVersion == MIGRATION_API_VERSION || apiVersion == AUTHMAN_RUNTIME_API_VERSION) {
            "runtime API $apiVersion is incompatible with the frozen bootstrap contract"
        }
        if (apiVersion == AUTHMAN_RUNTIME_API_VERSION) {
            require(contract == AUTHMAN_RUNTIME_CONTRACT) {
                "runtime contract ${contract.orEmpty().ifBlank { "<missing>" }} is incompatible with $AUTHMAN_RUNTIME_CONTRACT"
            }
        } else if (!contract.isNullOrBlank()) {
            require(contract == AUTHMAN_RUNTIME_CONTRACT) {
                "runtime contract $contract is incompatible with $AUTHMAN_RUNTIME_CONTRACT"
            }
        }
    }

    fun bootstrapOwnsCommand(apiVersion: Int?): Boolean =
        apiVersion == null || apiVersion == AUTHMAN_RUNTIME_API_VERSION
}
