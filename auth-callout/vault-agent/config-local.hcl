pid_file = "./pidfile"

exit_after_auth = true

#
vault {
   address = "${env:VAULT_ADDR:=http://127.0.0.1:8200}"
}
#

auto_auth {
   method "token" {
       type       = "jwt"
       namespace  = "${env:VAULT_NAMESPACE:=}"
       mount_path = "${env:VAULT_MOUNT_PATH:=auth/jwt}"
       config = {
          token = "${env:VAULT_ID_TOKEN:=}"
          role  = "${env:VAULT_ROLE:=}"
       }
   }
}

template {
   source      = "./vault-agent/templates/main.hcl"
   destination = "${env:VAULT_SECRETS_DEST:=./secrets}"
   error_on_missing_key = true
}
