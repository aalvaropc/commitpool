package mail

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

func TestSendInput_CarriesTextHTMLAndTheCalendar(t *testing.T) {
	in := sendInput("CommitPool <hello@example.test>", Message{
		To: "priya@example.test", Subject: "It’s on", Text: "plain", HTML: "<p>rich</p>",
		Attachments: []Attachment{{Name: "class.ics", ContentType: "text/calendar; charset=utf-8; method=PUBLISH", Data: []byte("BEGIN:VCALENDAR")}},
	})
	msg := in.Content.Simple
	if aws.ToString(in.FromEmailAddress) != "CommitPool <hello@example.test>" || in.Destination.ToAddresses[0] != "priya@example.test" {
		t.Errorf("from %q to %v", aws.ToString(in.FromEmailAddress), in.Destination.ToAddresses)
	}
	if aws.ToString(msg.Body.Text.Data) != "plain" || aws.ToString(msg.Body.Html.Data) != "<p>rich</p>" || aws.ToString(msg.Subject.Charset) != "UTF-8" {
		t.Errorf("body = %+v", msg.Body)
	}
	if len(msg.Attachments) != 1 || aws.ToString(msg.Attachments[0].FileName) != "class.ics" || string(msg.Attachments[0].RawContent) != "BEGIN:VCALENDAR" ||
		msg.Attachments[0].ContentDisposition != types.AttachmentContentDispositionAttachment {
		t.Errorf("attachments = %+v", msg.Attachments)
	}
}

func TestSendInput_TextOnlyHasNoHTMLPart(t *testing.T) {
	if in := sendInput("a@example.test", Message{To: "b@example.test", Subject: "s", Text: "t"}); in.Content.Simple.Body.Html != nil || len(in.Content.Simple.Attachments) != 0 {
		t.Errorf("simple = %+v", in.Content.Simple)
	}
}
