package edward

import "github.com/mattevans/edward/common"

func (c *Client) Version() string {
	return common.EdwardVersion
}
