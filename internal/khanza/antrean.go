package khanza

import "context"

// InsertTaskID mencatat task id persis format Khanza (no_rawat, taskid enum, waktu datetime)
// supaya modul antrean Khanza bisa melanjutkan task 4–7. Duplikat diabaikan (seperti menyimpantf2).
func (r *Repo) InsertTaskID(ctx context.Context, noRawat, taskID, waktu string) error {
	_, err := r.DB.ExecContext(ctx,
		"INSERT INTO referensi_mobilejkn_bpjs_taskid (no_rawat, taskid, waktu) VALUES (?,?,?)", noRawat, taskID, waktu)
	if IsDuplicate(err) {
		return nil
	}
	return err
}
