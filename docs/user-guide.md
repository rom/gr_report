# User guide

## Roles

| Role | Who | Can do |
| --- | --- | --- |
| `group` | One account per participating group (or person) | Write reports, answer requests, read reports sent to the group |
| `leader` | The exercise leader(s) | Work the report queue, send requests, create templates |
| `admin` | Whoever runs the tool | Create and manage accounts, create templates |

All exercise leaders share one queue. An account with the `group` role is
shared by the members of that group; its *display name* is what appears as
the sender of a report.

Times are shown in UTC (suffix `Z`).

## Logging in

Open the address you were given and log in with your username and password.
A new account has a **one-time password**: you must choose your own
password (at least 10 characters) before you can do anything else. After
five wrong attempts the login is blocked for a while.

Sessions end after 2 hours without activity and at the latest after
12 hours.

## For groups

### Write a report

1. On **Home**, pick a report template and press **Start report**.
2. Fill in the form.
   * **To** – the receiver. The default is the exercise leader; you can
     also address another group.
   * **Subject** and **Content** are always required.
   * A template can add extra fields (text, number, choice, checkbox, date
     and time). Fields marked `*` are required.
3. Press **Send report**. The server sets the timestamp. A sent report
   cannot be changed – send a new one if something needs correcting.

### Answer a request

Requests from the exercise leader are listed on **Home** and under
**Requests**. Press **Answer**: the form uses the template the leader
attached and the answer always goes to the exercise leader. You can answer
a request more than once until the leader closes it.

### Inbox and sent reports

* **Inbox** – reports other groups addressed to your group. It works like
  the leader's queue (see below).
* **Sent** – everything your group has sent.

## For the exercise leader

### The queue

**Queue** shows the incoming reports. The panel on the left has **one queue
per group** plus *All queues*; the red badge counts new (unread) reports.

* **Filter** by status, template, folder, request and date range.
* **Search** with one or more words; all words must occur in the subject,
  content, sender, template, folder or a field value.
* **Sort** by clicking a column heading; click again to reverse.
* **Auto-refresh** reloads the page every minute (switch it off while
  ticking check boxes).

A report has a status:

| Status | Meaning |
| --- | --- |
| `new` | Not opened yet (shown in bold) |
| `read` | Opened |
| `filed` | Handled and filed, optionally in a named folder |

Opening a report marks it as read. To **file** reports, tick them, choose
*file in folder*, type a folder name (existing names are suggested) and
press **Apply** – or use the **File** button on the report page. Filed
reports stay searchable; filter on *Status: filed* or on the folder to find
them again. *Mark as new* puts a report back at the top of your attention.

### Send a question or information request

**Requests → New request**: write the subject and the question, choose the
**report template** the groups shall answer with, and choose the recipients
(all groups or selected ones). The request page shows which groups have
answered; **Show responses in queue** opens the queue filtered on the
request. **Close request** stops further answers.

### Report templates

**Templates → New template**. Every report always has the timestamp,
sender, receiver, subject and content. A template adds:

* a name and instructions shown above the form,
* a default subject and a hint for the content field,
* extra fields. Each field has a label, a type (`text`, `textarea`,
  `number`, `select`, `checkbox`, `datetime`), a *required* flag, options
  (comma separated, for `select`) and a hint.

Clear the label of a field to remove it. Use **Save and add more fields**
for more empty rows. **Archive** hides a template from the groups; reports
already written keep their content, and open requests that use the template
can still be answered.

Changing a template never changes reports that were already sent.

## For the administrator

### First start

On the first start the server creates the account `admin` and writes its
one-time password to the file `initial-admin-password` in the data
directory (readable by the service user and root only). Log in, choose a
new password and delete the file.

### Accounts

**Accounts → New account**: choose username, display name and role. The
generated one-time password is shown **once** in the green message – hand
it to the group or leader.

For each account you can **Rename** (display name), **Reset password**
(new one-time password, all sessions of the account end) and **Disable** /
**Enable**. Accounts are never deleted, so old reports keep a valid sender.

A typical exercise set-up: one `leader` account per exercise leader and one
`group` account per group.
