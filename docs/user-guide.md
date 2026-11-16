# User guide

## Roles

| Role | Who | Can do |
| --- | --- | --- |
| `group` | One account per participating group (or person) | Write reports, answer requests, read reports sent to the group, chat |
| `leader` | The exercise leader(s) | Work the report queue, send requests, create templates, chat |
| `admin` | Whoever runs the tool | Create and manage accounts, create templates |

All exercise leaders share one queue. An account with the `group` role is
shared by the members of that group; its *display name* is what appears as
the sender of a report.

Times are shown in UTC (suffix `Z`).

## Language and appearance

Open **Settings** (also linked from the login page as *Language and
appearance*) to choose:

* the **language** – English or Swedish. Until you choose, your browser's
  language is used;
* the **colour theme** – system default, light, dark, high contrast, gold,
  spring, autumn, camouflage or console green.

Both are remembered in the browser you are using, not on the account, so
everyone sharing a group account can have their own. See
[accessibility.md](accessibility.md) for keyboard use, contrast and
screen-reader support.

This guide uses the English names of menus and buttons.

## Chat

**Chat** in the menu is for quick, informal messages; use reports for
anything that should be on record.

* **Everyone** – read by all groups and the exercise leader.
* **Exercise leader** – a direct conversation between your group and the
  exercise leaders. All leaders see it and any of them may answer.
* **A group's name** – a direct conversation between your group and that
  group. Nobody else can read it, not even the exercise leader.

The line under the heading always says who can read the conversation you
are looking at. Write in the box at the bottom and press **Send**. New
messages appear by themselves within a few seconds (reload the page if your
browser has JavaScript switched off). A red number next to a conversation,
and next to **Chat** in the menu, counts unread messages.

Remember that a group account is shared: everyone logged in to your group's
account sees the group's conversations. Messages cannot be edited or
deleted, are limited to 2 000 characters, and you can send at most 20 per
minute. **Show earlier messages** pages back through the history.

The person running the server can switch the chat off; the menu entry then
disappears.

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
* When new reports arrive while you have a page open, a notice appears at
  the top and the counter in the menu updates – nothing you are typing or
  have ticked is lost. Follow the link in the notice to see them.
* **Auto-refresh** reloads the whole page every minute instead. It is meant
  for browsers with JavaScript switched off; do not use it while ticking
  check boxes.
* The check box in the table heading selects all reports on the page.

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

Clear the label of a field to remove it. **Add field** gives another empty
row (without JavaScript: **Save and add more fields**). **Archive** hides a template from the groups; reports
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
