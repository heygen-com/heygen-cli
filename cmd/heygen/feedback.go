package main

import (
	"fmt"
	"unicode/utf8"

	"github.com/heygen-com/heygen-cli/internal/analytics"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
)

// maxFeedbackCommentLen bounds free-text so a stray log/stack-trace paste
// can't bloat the telemetry event. Generous enough for a detailed bug note;
// full reports belong in a GitHub issue, not this field.
const maxFeedbackCommentLen = 2000

type feedbackResponse struct {
	Rating  int    `json:"rating"`
	Comment string `json:"comment,omitempty"`
	Message string `json:"message"`
}

func newFeedbackCmd(ctx *cmdContext, analyticsClient *analytics.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "Send anonymous feedback about the CLI",
		Long: "Send anonymous feedback about your experience with the HeyGen CLI — a satisfaction " +
			"rating and an optional comment. Useful for agents to report whether a flow worked or to " +
			"flag a bug. Sent as an anonymous analytics event (no API key required) and honors the same " +
			"opt-out as usage analytics (HEYGEN_NO_ANALYTICS, or 'heygen config set analytics false').",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{"skipAuth": "true"},
		Example: "  # Rate your experience and add a note\n" +
			"  heygen feedback --rating 5 --comment \"avatar list --human is great\"\n\n" +
			"  # Report a problem\n" +
			"  heygen feedback --rating 2 --comment \"video download timed out on a large file\"",
		RunE: func(cmd *cobra.Command, args []string) error {
			rating, _ := cmd.Flags().GetInt("rating")
			comment, _ := cmd.Flags().GetString("comment")
			return runFeedback(ctx, analyticsClient, rating, comment)
		},
	}
	cmd.Flags().Int("rating", 0, "Satisfaction rating 1-5: 1 = broke / unusable, 3 = worked with friction, 5 = worked great")
	cmd.Flags().String("comment", "", "Optional details about your experience or the bug you hit (max 2000 characters)")
	_ = cmd.MarkFlagRequired("rating")
	return cmd
}

func runFeedback(ctx *cmdContext, analyticsClient *analytics.Client, rating int, comment string) error {
	if rating < 1 || rating > 5 {
		return clierrors.NewUsage("rating must be between 1 and 5")
	}
	if n := utf8.RuneCountInString(comment); n > maxFeedbackCommentLen {
		return clierrors.NewUsage(fmt.Sprintf("comment must be %d characters or fewer (got %d); shorten it, or ask a maintainer to open a GitHub issue for a detailed report", maxFeedbackCommentLen, n))
	}

	message := "Thanks for the feedback!"
	if !analyticsClient.Feedback(rating, comment) {
		message = "Analytics is disabled, so feedback was not sent. Unset HEYGEN_NO_ANALYTICS " +
			"(or run 'heygen config set analytics true') to enable it."
	}

	data, err := marshalData(feedbackResponse{Rating: rating, Comment: comment, Message: message})
	if err != nil {
		return err
	}
	return ctx.formatter.Data(data, "", nil)
}
