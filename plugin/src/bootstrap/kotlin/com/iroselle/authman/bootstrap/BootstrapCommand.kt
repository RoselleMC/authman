package com.iroselle.authman.bootstrap

import com.velocitypowered.api.command.SimpleCommand

class BootstrapCommand(
    private val router: RuntimeCommandRouter,
) : SimpleCommand {
    override fun execute(invocation: SimpleCommand.Invocation) {
        router.execute(router.velocityActor(invocation.source()), invocation.arguments().toList())
    }

    override fun suggest(invocation: SimpleCommand.Invocation): List<String> =
        router.suggest(router.velocityActor(invocation.source()), invocation.arguments().toList())

    override fun hasPermission(invocation: SimpleCommand.Invocation): Boolean =
        router.canUse(router.velocityActor(invocation.source()))
}
