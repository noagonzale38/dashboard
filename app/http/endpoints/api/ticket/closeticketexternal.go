package api

import (
	"net/http"
	"strconv"

	"github.com/TicketsBot-cloud/common/closerelay"
	"github.com/TicketsBot-cloud/dashboard/app"
	"github.com/TicketsBot-cloud/dashboard/database"
	"github.com/TicketsBot-cloud/dashboard/redis"
	"github.com/TicketsBot-cloud/dashboard/utils"
	"github.com/gin-gonic/gin"
)

// externalActorId is the hardcoded Discord user ID used as the actor when a
// ticket is closed via the external API. The external API is authenticated by
// a shared API key rather than a logged-in user, so all closes are attributed
// to this fixed identity.
const externalActorId uint64 = 1446630962644386033

// CloseTicketExternal closes a ticket in response to an API-key-authenticated
// request. It mirrors CloseTicket but skips the per-user permission check
// (authorization is the API key itself) and uses a hardcoded actor ID.
func CloseTicketExternal(c *gin.Context) {
	guildId, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.ErrorStr("Invalid guild ID provided: %s", c.Param("id")))
		return
	}

	ticketId, err := strconv.Atoi(c.Param("ticketId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.ErrorStr("Invalid ticket ID provided: %s", c.Param("ticketId")))
		return
	}

	var body closeBody
	// Body is optional for the external endpoint; ignore bind errors so callers
	// can send an empty body.
	_ = c.ShouldBindJSON(&body)

	ticket, err := database.Client.Tickets.Get(c, ticketId, guildId)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, app.NewError(err, "Unable to load ticket. Please try again."))
		return
	}

	if ticket.UserId == 0 {
		c.JSON(http.StatusNotFound, utils.ErrorStr("Ticket #%d not found", ticketId))
		return
	}

	data := closerelay.TicketClose{
		GuildId:  guildId,
		TicketId: ticket.Id,
		UserId:   externalActorId,
		Reason:   body.Reason,
	}

	if err := closerelay.Publish(redis.Client.Client, data); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, app.NewError(err, "Failed to publish ticket close event to Redis"))
		return
	}

	c.JSON(200, utils.SuccessResponse)
}
