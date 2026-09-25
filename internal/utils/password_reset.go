package utils

import "time"

// PasswordResetTokenTTL is how long an emailed password reset link stays valid.
const PasswordResetTokenTTL = time.Hour

// PasswordResetResendCooldown is how long after a reset email another request
// for the same account is answered without sending one, so the endpoint cannot
// be used to flood an inbox.
const PasswordResetResendCooldown = time.Minute
