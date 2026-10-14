package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
)

// applyDomainRosterAccess keeps domain-mode account provisioning consistent
// with the institute's imported roster. A non-matching person can still use a
// personal account, but their institution entitlement is removed. A matching
// verified account is attached to the institution only after roster match.
func applyDomainRosterAccess(ctx context.Context, db *sql.DB, userID, email string) (string, error) {
	at := strings.LastIndex(email, "@")
	if at < 1 {
		return "free", nil
	}
	domain := strings.ToLower(email[at+1:])

	var institutionID string
	err := db.QueryRowContext(ctx, `
        SELECT i.id::text
        FROM public.institutions i
        JOIN public.institution_domains d ON d.institution_id=i.id
        WHERE i.status='active' AND i.auth_mode='domain' AND d.domain=$1
          AND i.roster_enforced=TRUE
          AND EXISTS (SELECT 1 FROM public.institution_licenses l WHERE l.institution_id=i.id AND l.status='active' AND (l.expires_at IS NULL OR l.expires_at>NOW()))
        LIMIT 1
    `, domain).Scan(&institutionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} // no enforced roster; preserve existing behavior
	if err != nil {
		return "", err
	}

	hash, err := rosterEmailHash(email)
	if err != nil {
		return "", err
	}
	var allowed bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.institution_roster WHERE institution_id=$1::uuid AND email_hash=$2 AND active=TRUE)`, institutionID, hash).Scan(&allowed); err != nil {
		return "", err
	}
	if allowed {
		_, err = db.ExecContext(ctx, `UPDATE public.users SET institution_id=$2::uuid, plan='institution', updated_at=NOW() WHERE id=$1::uuid AND account_type='user' AND role='user'`, userID, institutionID)
		if err != nil {
			return "", err
		}
		return "institution", nil
	}
	// Remove only the institution entitlement; retain the user's personal account/history.
	_, err = db.ExecContext(ctx, `UPDATE public.users SET institution_id=NULL, plan='free', updated_at=NOW() WHERE id=$1::uuid AND account_type='user' AND role='user' AND institution_id=$2::uuid`, userID, institutionID)
	if err != nil {
		return "", err
	}
	return "free", nil
}

func logRosterAccessError(err error) { log.Printf("institution domain roster: %v", err) }
