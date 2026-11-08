package pb

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

// protoimpl round-trip 守卫：确保 User.f23 PayGrade / f24 FansClub 的字段号与类型
// 在编解码两侧一致（防止未来重排格号把 VIP/粉丝团等级悄悄打歪）。
func TestUserPayGradeFansClubRoundTrip(t *testing.T) {
	in := &User{
		Id: 123,
		PayGrade:  &PayGrade{Level: 18, Name: "帝王", TotalDiamondCount: 90000},
		FansClub:  &FansClub{Data: &FansClubData{Level: 7, ClubName: "某团", AnchorId: 555}},
	}
	b, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out User
	if err := proto.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := out.GetPayGrade().GetLevel(); got != 18 {
		t.Errorf("PayGrade.level = %d, want 18", got)
	}
	if got := out.GetFansClub().GetData().GetLevel(); got != 7 {
		t.Errorf("FansClub.data.level = %d, want 7", got)
	}
	if got := out.GetFansClub().GetData().GetClubName(); got != "某团" {
		t.Errorf("FansClub.data.clubName = %q, want 某团", got)
	}
}
