-- name: TfGetPlayerSession :one
select state
from tf_player_session
where player_id = $1;
