package linear

// GraphQL documents. Each is a named operation; the fixtures in the tests key on the name.
//
// Linear lists in descending order of the orderBy field ("to get most recently updated
// resources, order by updatedAt"), so comments(first: 20, orderBy: createdAt) is the newest 20.
// item() still sorts them newest-first, so a reversed order would only change which 20 come in.

const issueFields = `
fragment LinearIssueFields on Issue {
  id identifier url title description updatedAt
  state { id name type }
  assignee { id name displayName email }
  parent { id identifier }
  team { id key }
  labels(first: 50) { nodes { id name } }
  comments(first: 20, orderBy: createdAt) { nodes { id body createdAt user { name displayName } } }
  attachments(first: 20) { nodes { title url } }
}`

const (
	qViewer = `query LinearViewer { viewer { id name displayName email } }`

	qTeam = `query LinearTeam($team: String!) {
  teams(first: 2, filter: { or: [{ key: { eqIgnoreCase: $team } }, { name: { eqIgnoreCase: $team } }] }) {
    nodes { id key name }
  }
}`

	qStates = `query LinearStates($id: String!) {
  team(id: $id) { id states(first: 100) { nodes { id name type position } } }
}`

	qAssigned = `query LinearAssigned($filter: IssueFilter, $after: String) {
  viewer {
    assignedIssues(first: 50, after: $after, filter: $filter, orderBy: updatedAt) {
      pageInfo { hasNextPage endCursor }
      nodes { ...LinearIssueFields }
    }
  }
}` + issueFields

	qChildren = `query LinearChildren($filter: IssueFilter, $after: String) {
  issues(first: 50, after: $after, filter: $filter, orderBy: updatedAt) {
    pageInfo { hasNextPage endCursor }
    nodes { ...LinearIssueFields }
  }
}` + issueFields

	qIssues = `query LinearIssues($filter: IssueFilter, $after: String) {
  issues(first: 50, after: $after, filter: $filter, orderBy: updatedAt) {
    pageInfo { hasNextPage endCursor }
    nodes { ...LinearIssueFields }
  }
}` + issueFields

	qIssue = `query LinearIssue($id: String!) { issue(id: $id) { ...LinearIssueFields } }` + issueFields

	qIssueID = `query LinearIssueID($id: String!) { issue(id: $id) { id } }`

	// qIssueTeam reads what Move and SetLabels resolve names against: the issue's own labels, its
	// team's states and labels, and the workspace labels (no team).
	qIssueTeam = `query LinearIssueTeam($id: String!) {
  issue(id: $id) {
    id
    labels(first: 100) { nodes { id name } }
    team { id states(first: 100) { nodes { id name type } } labels(first: 250) { nodes { id name } } }
  }
  issueLabels(first: 250, filter: { team: { null: true } }) { nodes { id name } }
}`

	qTeamMeta = `query LinearTeamMeta($id: String!) {
  team(id: $id) { id states(first: 100) { nodes { id name type } } labels(first: 250) { nodes { id name } } }
  issueLabels(first: 250, filter: { team: { null: true } }) { nodes { id name } }
}`

	mIssueUpdate = `mutation LinearIssueUpdate($id: String!, $input: IssueUpdateInput!) {
  issueUpdate(id: $id, input: $input) { success }
}`

	mCommentCreate = `mutation LinearCommentCreate($input: CommentCreateInput!) {
  commentCreate(input: $input) { success comment { id } }
}`

	mIssueCreate = `mutation LinearIssueCreate($input: IssueCreateInput!) {
  issueCreate(input: $input) { success issue { ...LinearIssueFields } }
}` + issueFields

	mAddLabel = `mutation LinearIssueAddLabel($id: String!, $labelId: String!) {
  issueAddLabel(id: $id, labelId: $labelId) { success }
}`

	mRemoveLabel = `mutation LinearIssueRemoveLabel($id: String!, $labelId: String!) {
  issueRemoveLabel(id: $id, labelId: $labelId) { success }
}`
)
