# ChaosLab — Development Backup README

This document records the changes completed during the current development session on **16 October 2026**. It is intended as a backup reference for the project, a handoff document for future development, and a guide for testing and deployment.

## 1. Project overview

ChaosLab is a seeded cybersecurity incident simulator. Users investigate simulated server incidents by reading logs, identifying suspicious source IP addresses, recognizing attack behavior, and submitting their findings.

The project contains three main components:

- `frontend/` — static HTML, CSS and JavaScript pages.
- `auth-api/` — Go authentication and account-management API.
- `core-api/` — Go simulation, submission, history, classroom and multiplayer API.

The normal gameplay flow is:

1. A user registers and verifies their email.
2. The user logs in and receives an authenticated session token.
3. The dashboard allows the user to select Singleplayer, Multiplayer or Classroom mode.
4. A simulation is generated with a difficulty and seed.
5. The mission page displays simulated files and server logs.
6. The user investigates the logs and reports attackers, endpoints and anomaly types.
7. The core API evaluates the submission and returns a score and field-level results.
8. Results are stored in history or associated with a multiplayer/classroom activity.
9. The user can rate the mission and submit feedback for administrator review.

## 2. Changes completed today

### 2.1 About Us page

Created:

- `frontend/about.html`

The page describes ChaosLab as a practical incident-response learning platform. It explains:

- The purpose of the simulator.
- Investigation-based learning.
- Singleplayer, multiplayer and classroom experiences.
- The value of seeded scenarios and repeatable exercises.
- How ChaosLab helps users develop practical log-analysis skills.

The page uses the existing ChaosLab terminal-inspired design and shared navigation.

### 2.2 Contact Us page

Created:

- `frontend/contact.html`

The Contact Us page includes the official contact address:

`suk2324071@sicsr.ac.in`

The address is clickable through a `mailto:` link. The page also explains the types of enquiries users can send:

- Feedback
- Technical support
- Bug reports
- Collaboration enquiries
- Academic or institutional usage

### 2.3 Shared informational styling

Created:

- `frontend/css/info.css`

This stylesheet is shared by the About Us, Contact Us and Documentation pages. It includes:

- Responsive cards.
- Callout sections.
- Contact panels.
- Plan cards.
- Documentation step layouts.
- Mobile responsive behavior.

### 2.4 Home page footer links

Updated:

- `frontend/index.html`

The home page footer now links to:

- About Us
- Contact Us
- Documentation
- Login
- Registration

### 2.5 Plans and subscriptions section

The About Us page now documents the three available plans:

#### Free

- Default plan for every new account.
- Monthly limits for Easy, Medium and Hard singleplayer games.
- Limited room creation for multiplayer.
- Suitable for beginners and occasional practice.

#### Pro

- Available after an administrator reviews a request.
- Provides higher monthly game limits.
- Provides higher room-creation limits.
- Designed for frequent individual practice.

#### Institution

- Intended for partner institutions, students and faculty.
- Provides custom limits.
- Supports classrooms and shared results.
- Supports institution-managed access and learning activities.

The page also explains that joining a multiplayer room is free and that only creating a room counts toward the creator’s room limit. Monthly limits reset on the first day of the month at 00:00 UTC.

### 2.6 Documentation and beginner tutorial page

Created:

- `frontend/documentation.html`

The documentation page provides a beginner-friendly walkthrough covering:

1. Creating and verifying an account.
2. Starting a singleplayer mission.
3. Reading the mission brief and server logs.
4. Identifying source IPs, request paths, status codes and suspicious patterns.
5. Submitting findings.
6. Reviewing scores and history.
7. Multiplayer room usage.
8. Institution classroom usage.

The page includes a beginner tip explaining that users should read the mission file first because it indicates how many objectives must be found.

### 2.7 Mission feedback and rating form

The mission result dialog now includes a feedback form with:

- A required rating from 1 to 5.
- Optional comments up to 1,000 characters.
- Mission difficulty.
- Simulation seed.
- Mission score.

The form appears after a mission submission has been processed. Empty ratings are rejected in the browser. A valid feedback submission is sent to the authentication API and stored for administrator review.

Updated frontend files:

- `frontend/mission.html`
- `frontend/js/two.js`
- `frontend/js/auth-api.js`
- `frontend/css/two.css`

The original email-only behavior was replaced with persistent API-backed feedback. The administrator can therefore see feedback inside the Admin panel instead of relying only on an email client.

### 2.8 Persistent feedback database table

Created:

- `auth-api/migrations/011_mission_feedback.sql`

The migration creates the `public.mission_feedback` table with:

- Feedback ID.
- User ID.
- Rating from 1 to 5.
- Mission difficulty.
- Simulation seed.
- Mission score.
- User comment.
- Submission timestamp.

The table includes validation constraints and an index for recent feedback. It also enables PostgreSQL row-level security, consistent with the project’s existing database setup.

### 2.9 Feedback API endpoints

Created:

- `auth-api/pkg/handlers/feedback.go`

Added authenticated endpoints:

```text
POST /api/v1/auth/feedback
GET  /api/v1/auth/admin/feedback
```

The POST endpoint:

- Requires a valid logged-in session.
- Validates the rating.
- Validates score ranges.
- Limits comment length.
- Stores the feedback in PostgreSQL.

The GET endpoint:

- Requires a valid authenticated session.
- Checks that the user has the admin role.
- Returns recent feedback submissions.
- Includes user, rating, score, difficulty, seed, comment and timestamp information.

Updated API routing files:

- `auth-api/api/auth/plan/index.go`
- `auth-api/vercel.json`

### 2.10 Admin feedback panel

Updated:

- `frontend/admin.html`
- `frontend/js/admin.js`
- `frontend/css/admin.css`

The Admin page now contains a Mission feedback section. Each feedback card displays:

- User name.
- User email.
- Submission date.
- Rating.
- Mission score.
- Difficulty.
- Simulation seed.
- User comment.

Feedback is loaded through the admin-only API endpoint. Non-admin users are redirected or rejected by the existing authentication and authorization flow.

### 2.11 First-login onboarding tour

Created:

- `frontend/js/onboarding.js`
- `frontend/css/onboarding.css`

Updated:

- `frontend/dashboard.html`

The dashboard now includes a guided onboarding tour for new users. The tour contains these stages:

1. Welcome to ChaosLab.
2. Start a Singleplayer investigation.
3. Follow patterns in the evidence.
4. Use Multiplayer or Classroom mode.
5. Understand monthly usage limits.
6. Review history and profile settings.
7. Start the first incident.

The onboarding experience includes:

- Spotlight highlighting of dashboard controls.
- Progress dots.
- Step counter.
- Next and Back buttons.
- Skip tour button.
- Finish button.
- Keyboard navigation with Enter, arrow keys and Escape.
- Responsive mobile positioning.
- Reduced-motion accessibility support.
- A replay button labelled `Take the tour`.

Completion is stored per user with a key similar to:

```text
chaoslab_onboarding_v1:<user-id>
```

This prevents the tutorial from repeatedly appearing for returning users while still allowing the user to replay it manually.

### 2.12 Searchable and filterable access logs

Updated:

- `frontend/mission.html`
- `frontend/js/two.js`
- `frontend/css/two.css`

When the user opens `/var/log/nginx/access.log`, a filter toolbar is displayed with:

- IP address filter.
- Status code filter.
- HTTP method filter.
- Request path filter.
- Clear filters button.

Filtering is performed in the browser against the parsed access-log rows. The interface updates immediately and displays the number of matching entries.

Examples:

```text
IP address: 203.0.113.7
Status code: 401
HTTP method: POST
Request path: /login
```

Multiple filters can be combined. If no entries match, the table displays a clear empty-result message instead of showing a blank screen.

### 2.13 Improved result analysis

Updated:

- `frontend/mission.html`
- `frontend/js/two.js`
- `frontend/css/two.css`

After a mission submission, the result dialog now provides a result-analysis panel containing:

- Score out of 100.
- Objectives matched.
- Accuracy percentage.
- Investigation time.
- Field-level breakdown for every submitted finding.
- Source IP correctness.
- Endpoint/path correctness.
- Anomaly-type correctness.
- Guidance when objectives remain incomplete.

The analysis uses the existing server response and does not reveal hidden answer data. This preserves the challenge while giving useful feedback about the user’s submitted evidence.

## 3. Important files added or updated

### New files

```text
frontend/about.html
frontend/contact.html
frontend/documentation.html
frontend/css/info.css
frontend/css/onboarding.css
frontend/js/onboarding.js
auth-api/migrations/011_mission_feedback.sql
auth-api/pkg/handlers/feedback.go
Backup-sk_Readme.md
```

### Main updated files

```text
frontend/index.html
frontend/dashboard.html
frontend/mission.html
frontend/admin.html
frontend/js/auth-api.js
frontend/js/admin.js
frontend/js/two.js
frontend/css/admin.css
frontend/css/two.css
auth-api/api/auth/plan/index.go
auth-api/vercel.json
```

## 4. Running the frontend locally

The frontend is a static site and does not require a build step.

From PowerShell:

```powershell
cd "E:\SEM VII\Dissertation Project\Project\chaosLab\frontend"
python -m http.server 5500
```

Open:

```text
http://localhost:5500/index.html
```

Useful pages:

```text
http://localhost:5500/dashboard.html
http://localhost:5500/mission.html
http://localhost:5500/about.html
http://localhost:5500/contact.html
http://localhost:5500/documentation.html
http://localhost:5500/admin.html
```

Do not open the pages directly with `file:///...`. Use an HTTP server so browser storage, relative assets and API requests behave correctly.

## 5. Feedback feature deployment requirements

The feedback API requires the database migration to be applied before the deployed frontend can save feedback:

```text
auth-api/migrations/011_mission_feedback.sql
```

The updated `auth-api` must also be deployed so that the following routes exist:

```text
/api/v1/auth/feedback
/api/v1/auth/admin/feedback
```

The authentication API still requires its normal environment variables, including:

- `DATABASE_URL`
- `UPSTASH_REDIS_REST_URL`
- `UPSTASH_REDIS_REST_TOKEN`
- `CORS_ALLOWED_ORIGIN`
- Any existing email and application URL variables required by the deployment.

The frontend’s deployed authentication API URL is configured in:

```text
frontend/js/auth-api.js
```

## 6. Testing completed

The following automated checks were completed:

- JavaScript syntax checks passed for all 28 frontend JavaScript files.
- HTML asset-reference checks passed for all 23 HTML pages.
- Local HTTP smoke tests successfully loaded the updated pages and assets.
- Onboarding integration markers were verified.
- Search/filter integration markers were verified.
- Result-analysis integration markers were verified.
- `auth-api/vercel.json` was parsed successfully as JSON.

The Go test suite could not be executed in the current environment because the Go toolchain is not installed. The backend should be tested after installing Go or through the normal CI/deployment environment.

Recommended manual tests after deployment:

1. Register a new user.
2. Log in and verify that the onboarding tour opens.
3. Test Next, Back, Skip and Finish.
4. Reload the dashboard and confirm that the tour does not automatically reopen.
5. Click `Take the tour` and confirm it reopens.
6. Start a mission and open the access log.
7. Filter by IP, status, method and path separately.
8. Combine multiple filters.
9. Clear filters.
10. Submit both an incomplete and complete mission.
11. Confirm the result-analysis panel appears.
12. Submit a rating and comment.
13. Log in as an admin.
14. Confirm the feedback appears in the Admin panel.

To reset onboarding during testing, run this in the browser console on the dashboard:

```js
Object.keys(localStorage)
  .filter(key => key.startsWith("chaoslab_onboarding_v1:"))
  .forEach(key => localStorage.removeItem(key));
location.reload();
```

## 7. Known limitations

- Interactive browser testing was not possible in the development environment because Chrome, Edge, Firefox and Playwright were unavailable.
- Feedback will not appear in the Admin panel until migration `011_mission_feedback.sql` has been applied and the updated auth API has been deployed.
- The result analysis can report which submitted fields were correct, but it intentionally does not reveal the exact hidden missed answers.
- The current log filters operate on the rows already loaded in the browser.
- The frontend is static and currently has no bundler or automated browser-test framework.

## 8. Future possible upgrades

### High-priority upgrades

#### Admin feedback analytics

Add charts and summaries for:

- Average rating.
- Rating distribution.
- Ratings by difficulty.
- Ratings over time.
- Most common feedback keywords.
- Missions with unusually low ratings.

#### Advanced log investigation

Add:

- Date/time range filters.
- Status-code groups such as 2xx, 3xx, 4xx and 5xx.
- Multi-select IP filtering.
- Saved filters.
- Highlighting of matching text.
- Log export for study notes.
- Keyboard shortcuts for filtering.

#### Better result history

Add:

- Per-mission result pages.
- Score trends over time.
- Average completion time.
- Accuracy by difficulty.
- Personal best records.
- Comparison between first attempt and later attempts.

#### Richer learning feedback

Add explanations after submission for:

- Why a request is suspicious.
- What credential brute force looks like.
- How endpoint scanning appears in logs.
- How path traversal attempts are recognized.
- Why particular status codes matter.

The explanations should be shown after grading so the challenge is not spoiled before submission.

### Medium-priority upgrades

#### Achievement and progression system

Possible achievements include:

- First mission completed.
- Perfect score.
- Hard mission completed.
- Ten missions completed.
- Multiplayer winner.
- Three consecutive correct investigations.

#### Better onboarding

Extend the onboarding system with:

- A guided first mission.
- Optional hints.
- A beginner mode with explanations.
- Progress tracking across documentation sections.
- Contextual help on the mission page.

#### Documentation search

Add searchable topics for:

- HTTP status codes.
- Nginx access logs.
- Source IP addresses.
- Authentication failures.
- Credential brute force.
- Endpoint scanning.
- Path traversal.

#### Notification center

Add a shared notification component for:

- Successful submissions.
- Expired sessions.
- Feedback saved confirmation.
- Plan decisions.
- Classroom invitations.
- Multiplayer events.

### Long-term upgrades

#### Real-time multiplayer updates

Replace polling with WebSockets or Server-Sent Events for:

- Player joining and leaving.
- Room countdowns.
- Live score updates.
- Host actions.
- Match completion.

#### Instructor analytics

For institution accounts, add:

- Class performance summaries.
- Student progress tracking.
- Difficulty recommendations.
- Exportable reports.
- Assignment deadlines.
- Instructor-authored missions.

#### Scenario authoring system

Allow authorized instructors or administrators to create scenarios with:

- Custom log files.
- Custom mission objectives.
- Difficulty configuration.
- Attack-pattern templates.
- Expected findings.
- Automatic validation tests.

#### Accessibility and localization

Improve:

- Screen-reader announcements.
- Focus management in dialogs.
- High-contrast mode.
- Larger text mode.
- Reduced-motion support across all pages.
- Multiple language support.

#### Automated testing infrastructure

Add:

- Go unit tests for feedback handlers.
- API integration tests using a test database.
- Browser tests with Playwright.
- Accessibility tests.
- Regression tests for mission filtering.
- End-to-end tests for feedback appearing in the Admin panel.

## 9. Suggested next implementation order

The recommended order for the next development phase is:

1. Apply and verify the feedback database migration.
2. Deploy the updated authentication API.
3. Add browser-based end-to-end tests.
4. Add admin feedback analytics.
5. Add score history charts.
6. Add contextual hints and beginner explanations.
7. Improve log filtering with status groups and saved filters.
8. Add achievements and progression.

