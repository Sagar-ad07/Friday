package com.friday.android

import android.content.Context
import android.graphics.Bitmap
import android.hardware.display.DisplayManager
import android.hardware.display.VirtualDisplay
import android.media.Image
import android.media.ImageReader
import android.media.projection.MediaProjection
import android.util.Base64
import android.util.DisplayMetrics
import android.view.WindowManager
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.io.ByteArrayOutputStream
import java.security.MessageDigest

/**
 * Screen-capture eye. Given a MediaProjection (obtained via the system permission
 * dialog in MainActivity), it grabs frames, downsamples, change-gates, and uploads
 * to /device/{id}/screenshot (which also feeds Friday's eye). Mirrors phone_eye.html.
 */
class ScreenEye(
    private val ctx: Context,
    private val projection: MediaProjection,
    private val api: ApiClient,
    private val deviceId: String,
) {
    private var reader: ImageReader? = null
    private var vdisplay: VirtualDisplay? = null
    private val scope = CoroutineScope(Dispatchers.IO)
    private var lastHash = ""

    fun start(intervalMs: Long = 4000) {
        val wm = ctx.getSystemService(Context.WINDOW_SERVICE) as WindowManager
        val metrics = DisplayMetrics()
        @Suppress("DEPRECATION") wm.defaultDisplay.getRealMetrics(metrics)
        val w = (metrics.widthPixels * 0.35f).toInt().coerceAtLeast(360)
        val h = (metrics.heightPixels * 0.35f).toInt().coerceAtLeast(640)
        reader = ImageReader.newInstance(w, h, android.graphics.PixelFormat.RGBA_8888, 2)
        vdisplay = projection.createVirtualDisplay(
            "FridayEye", w, h, metrics.densityDpi,
            DisplayManager.VIRTUAL_DISPLAY_FLAG_AUTO_MIRROR,
            reader!!.surface, null, null
        )
        scope.launch {
            while (isActive) {
                delay(intervalMs)
                captureAndUpload()
            }
        }
    }

    private fun captureAndUpload() {
        val image = reader?.acquireLatestImage() ?: return
        try {
            val bmp = imageToBitmap(image) ?: return
            val stream = ByteArrayOutputStream()
            bmp.compress(Bitmap.CompressFormat.JPEG, 55, stream)
            val bytes = stream.toByteArray()
            val hash = md5(bytes)
            if (hash == lastHash) return // change-gated: skip unchanged frames
            lastHash = hash
            val b64 = Base64.encodeToString(bytes, Base64.NO_WRAP)
            api.postScreenshot(deviceId, b64)
        } catch (_: Exception) {} finally { image.close() }
    }

    private fun imageToBitmap(image: Image): Bitmap? {
        val plane = image.planes.firstOrNull() ?: return null
        val buffer = plane.buffer
        val pixelStride = plane.pixelStride
        val rowStride = plane.rowStride
        val rowPadding = rowStride - pixelStride * image.width
        val bmp = Bitmap.createBitmap(
            image.width + rowPadding / pixelStride, image.height, Bitmap.Config.ARGB_8888
        )
        bmp.copyPixelsFromBuffer(buffer)
        return Bitmap.createBitmap(bmp, 0, 0, image.width, image.height)
    }

    private fun md5(b: ByteArray): String =
        MessageDigest.getInstance("MD5").digest(b).joinToString("") { "%02x".format(it) }

    fun stop() {
        vdisplay?.release()
        reader?.close()
        projection.stop()
    }
}
