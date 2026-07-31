package friday

import (
	"io"

	"github.com/gin-gonic/gin"
)

// Smooth voice handlers
func AddSmoothVoiceHandlers(server *Server, voiceSystem *SmoothVoiceSystem) {
	router := server.Router()

	// Voice command processing
	router.POST("/voice/command", func(c *gin.Context) {
		var req struct {
			AudioData []byte `json:"audio_data"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "invalid audio data"})
			return
		}

		resp, err := voiceSystem.ProcessVoiceCommand(c.Request.Context(), req.AudioData)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, resp)
	})

	// Get voice status
	router.GET("/voice/status", func(c *gin.Context) {
		stats := voiceSystem.GetStats()
		c.JSON(200, gin.H{"status": "ok", "stats": stats})
	})

	// Stream voice response
	router.POST("/voice/stream", func(c *gin.Context) {
		var req struct {
			Text string `json:"text"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "text required"})
			return
		}

		ch, err := voiceSystem.StreamVoiceResponse(c.Request.Context(), req.Text)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		c.Stream(func(w io.Writer) bool {
			for text := range ch {
				c.Writer.WriteString("data: " + text + "\n\n")
				c.Writer.Flush()
			}
			return false
		})
	})

	// Cleanup old cache
	router.POST("/voice/cleanup", func(c *gin.Context) {
		voiceSystem.Cleanup()
		c.JSON(200, gin.H{"status": "ok", "message": "cache cleaned"})
	})
}
