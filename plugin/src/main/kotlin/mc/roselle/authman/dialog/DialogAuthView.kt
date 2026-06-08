package mc.roselle.authman.dialog

import com.github.retrooper.packetevents.PacketEvents
import com.github.retrooper.packetevents.protocol.dialog.CommonDialogData
import com.github.retrooper.packetevents.protocol.dialog.DialogAction
import com.github.retrooper.packetevents.protocol.dialog.MultiActionDialog
import com.github.retrooper.packetevents.protocol.dialog.action.DynamicCustomAction
import com.github.retrooper.packetevents.protocol.dialog.body.PlainMessage
import com.github.retrooper.packetevents.protocol.dialog.body.PlainMessageDialogBody
import com.github.retrooper.packetevents.protocol.dialog.button.ActionButton
import com.github.retrooper.packetevents.protocol.dialog.button.CommonButtonData
import com.github.retrooper.packetevents.protocol.dialog.input.Input
import com.github.retrooper.packetevents.protocol.dialog.input.TextInputControl
import com.github.retrooper.packetevents.protocol.nbt.NBTCompound
import com.github.retrooper.packetevents.protocol.nbt.NBTString
import com.github.retrooper.packetevents.resources.ResourceLocation
import com.github.retrooper.packetevents.wrapper.play.client.WrapperPlayClientCustomClickAction
import com.github.retrooper.packetevents.wrapper.play.server.WrapperPlayServerShowDialog
import com.velocitypowered.api.proxy.Player
import net.kyori.adventure.text.Component

class DialogAuthView {
    fun showPasswordDialog(player: Player, sessionId: String, displayName: String) {
        val additions = NBTCompound()
        additions.setTag("session", NBTString(sessionId))
        val submit = ActionButton(
            CommonButtonData(Component.text("Sign in"), Component.text("Submit Authman password"), 150),
            DynamicCustomAction(SUBMIT_PASSWORD, additions),
        )
        val exit = ActionButton(
            CommonButtonData(Component.text("Cancel"), Component.text("Disconnect"), 100),
            DynamicCustomAction(CANCEL, additions.copy()),
        )
        val common = CommonDialogData(
            Component.text("Authman Login"),
            Component.text("Authman"),
            false,
            true,
            DialogAction.WAIT_FOR_RESPONSE,
            listOf(
                PlainMessageDialogBody(PlainMessage(Component.text("Account: $displayName"), 340)),
                PlainMessageDialogBody(PlainMessage(Component.text("Enter your Authman password to continue."), 340)),
            ),
            listOf(
                Input(
                    PASSWORD_KEY,
                    TextInputControl(340, Component.text("Password"), true, "", 128, null),
                ),
            ),
        )
        val dialog = MultiActionDialog(common, listOf(submit), exit, 1)
        PacketEvents.getAPI().playerManager.sendPacket(player, WrapperPlayServerShowDialog(dialog))
    }

    companion object {
        val SUBMIT_PASSWORD = ResourceLocation("authman", "submit_password")
        val CANCEL = ResourceLocation("authman", "cancel")
        const val PASSWORD_KEY = "authman_password"

        fun readSubmission(packet: WrapperPlayClientCustomClickAction): DialogSubmission? {
            val id = packet.id
            if (id != SUBMIT_PASSWORD && id != CANCEL) {
                return null
            }
            val payload = packet.payload as? NBTCompound ?: NBTCompound()
            return DialogSubmission(
                action = if (id == CANCEL) DialogActionType.CANCEL else DialogActionType.SUBMIT_PASSWORD,
                sessionId = payload.getStringTagValueOrDefault("session", ""),
                password = payload.getStringTagValueOrDefault(PASSWORD_KEY, "")
                    .ifEmpty { payload.getStringTagValueOrDefault("password", "") }
                    .ifEmpty { payload.getStringTagValueOrDefault("value", "") },
            )
        }
    }
}

data class DialogSubmission(
    val action: DialogActionType,
    val sessionId: String,
    val password: String,
)

enum class DialogActionType {
    SUBMIT_PASSWORD,
    CANCEL,
}
