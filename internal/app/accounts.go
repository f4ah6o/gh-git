package app

import (
	"context"
	"fmt"
)

func (a *App) Accounts(ctx context.Context, host string) error {
	accounts, err := a.Auth.Accounts(ctx, host)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		_, err := fmt.Fprintf(a.Out, "No stored GitHub accounts found for %s.\n", host)
		return err
	}
	if _, err := fmt.Fprintln(a.Out, "HOST\tACCOUNT\tACTIVE\tSTATE\tTOKEN SOURCE"); err != nil {
		return err
	}
	for _, account := range accounts {
		active := "no"
		if account.Active {
			active = "yes"
		}
		if _, err := fmt.Fprintf(a.Out, "%s\t%s\t%s\t%s\t%s\n", account.Host, account.Login, active, account.State, account.TokenSource); err != nil {
			return err
		}
	}
	return nil
}
