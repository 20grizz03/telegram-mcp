package tgclient

import "testing"

func TestBuildLink(t *testing.T) {
	tests := []struct {
		name string
		args LinkArgs
		want string
	}{
		{
			name: "public channel",
			args: LinkArgs{Kind: KindChannel, Username: "durov", MsgID: 42},
			want: "https://t.me/durov/42",
		},
		{
			name: "public channel in topic",
			args: LinkArgs{Kind: KindChannel, Username: "mygroup", MsgID: 99, TopicID: 7},
			want: "https://t.me/mygroup/7/99",
		},
		{
			name: "private channel",
			args: LinkArgs{Kind: KindChannel, ChannelID: 1234567890, MsgID: 12},
			want: "https://t.me/c/1234567890/12",
		},
		{
			name: "private channel in topic",
			args: LinkArgs{Kind: KindChannel, ChannelID: 1234567890, MsgID: 12, TopicID: 5},
			want: "https://t.me/c/1234567890/5/12",
		},
		{
			name: "user has no link",
			args: LinkArgs{Kind: KindUser, MsgID: 1},
			want: "",
		},
		{
			name: "basic group has no link",
			args: LinkArgs{Kind: KindChat, MsgID: 1},
			want: "",
		},
		{
			name: "no message id",
			args: LinkArgs{Kind: KindChannel, Username: "x", MsgID: 0},
			want: "",
		},
		{
			name: "private without channel id",
			args: LinkArgs{Kind: KindChannel, MsgID: 5},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildLink(tt.args); got != tt.want {
				t.Errorf("BuildLink() = %q, want %q", got, tt.want)
			}
		})
	}
}
