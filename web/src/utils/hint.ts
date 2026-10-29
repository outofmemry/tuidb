// hint turns a driver's error into one plain sentence about what to check; the full text stays behind "Technical details".
export function hint(detail: string): string {
  const t = detail.toLowerCase()
  // the server is up but out of connection slots. Checked first: the same error text can carry words of the TLS and auth lines below
  if (/too many (clients|connections)|53300|remaining connection slots|max_connections|error 1040/.test(t)) return 'The server has no free connections right now ("too many clients"). Close other connections to it, or raise its max_connections, then try again.'
  if (/connection refused|econnrefused|actively refused/.test(t)) return 'Nothing answered at that address. Check that the database server is running, and that the host and port are right.'
  if (/no such host|name resolution|getaddrinfo|lookup .*: no such|server misbehaving/.test(t)) return 'That host name could not be found. Check the spelling of the host.'
  if (/password authentication failed|access denied|authentication failed|auth failed|wrong password|invalid password|not authorized|unauthorized|requires authentication|noauth|sasl|28p01|28000|1045/.test(t)) return 'The user name or password was not accepted. Check them, or ask whoever runs the database.'
  if (/does not exist|unknown database|no such database|database .* not found|3d000/.test(t)) return 'That database does not exist on the server. Check the database name.'
  if (/timeout|timed out|deadline exceeded/.test(t)) return 'The server did not answer in time. Check the host and port, and that the network or VPN is up.'
  if (/\bssl\b|\btls\b|certificate|x509/.test(t)) return 'The secure connection could not be set up. Check the SSL settings of this connection.'
  if (/permission denied|not permitted|insufficient privilege/.test(t)) return 'This user is not allowed to do that on the server.'
  return 'Check the connection details and try again.'
}
