package com.iroselle.authman.bridge

import org.bukkit.Bukkit
import org.bukkit.command.CommandSender
import org.bukkit.entity.Player
import org.bukkit.plugin.Plugin

class FoliaDispatcher(
    private val plugin: Plugin,
) {
    private val folia = runCatching {
        Class.forName("io.papermc.paper.threadedregions.RegionizedServer")
    }.isSuccess

    fun execute(sender: CommandSender, task: () -> Unit) {
        if (sender is Player) {
            execute(sender, task)
            return
        }
        if (folia) {
            runCatching {
                val scheduler = plugin.server.javaClass.getMethod("getGlobalRegionScheduler").invoke(plugin.server)
                val execute = scheduler.javaClass.methods.first {
                    it.name == "execute" && it.parameterCount == 2
                }
                execute.invoke(scheduler, plugin, Runnable(task))
            }.onFailure { plugin.logger.warning("Failed to schedule an Authman bridge response on Folia: ${it.message}") }
            return
        }
        executeBukkit(task)
    }

    fun execute(player: Player, task: () -> Unit): Boolean {
        if (!folia) {
            executeBukkit(task)
            return true
        }
        return runCatching {
            val scheduler = player.javaClass.getMethod("getScheduler").invoke(player)
            val execute = scheduler.javaClass.methods.first {
                it.name == "execute" && it.parameterCount == 4
            }
            execute.invoke(scheduler, plugin, Runnable(task), null, 1L) as Boolean
        }.onFailure {
            plugin.logger.warning("Failed to schedule an Authman bridge task for ${player.name} on Folia: ${it.message}")
        }.getOrDefault(false)
    }

    private fun executeBukkit(task: () -> Unit) {
        if (Bukkit.isPrimaryThread()) {
            task()
        } else {
            plugin.server.scheduler.runTask(plugin, Runnable(task))
        }
    }
}
