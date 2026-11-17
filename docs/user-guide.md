# User guide

## Roles

| Role | Who | Can do |
| --- | --- | --- |
| `group` | A participating group, and each of its members | Write reports, answer requests, read reports sent to the group, chat |
| `leader` | The exercise leader(s) | Work the report queue, send requests, create templates, chat, start a new exercise |
| `admin` | Whoever runs the tool | Manage accounts, create templates, make backups, read the audit log, start a new exercise |

All exercise leaders share one queue.

A group has its own account, which can be used as a **shared login**, and
can have **personal accounts** for its members. Whichever you log in with,
you act as the group: you see the group's requests, inbox and sent reports
and chat as the group. With a personal account your name is recorded too:
a report shows *From: Group Alpha* and *Written by: Anna Andersson*, and
your chat messages are signed *Anna Andersson (Group Alpha)*. What you have
read, your drafts and your saved filters are your own.

Times are shown in the time zone you choose under **Settings** (see below);
a time ending in `Z` is UTC.

## Language and appearance

Open **Settings** (also linked from the login page as *Language and
appearance*) to choose:

* the **language** – English or Swedish. Until you choose, your browser's
  language is used;
* the **colour theme** – system default, light, dark, high contrast, gold,
  spring, autumn, camouflage or console green;
* the **time zone** – a name such as `Europe/Stockholm`, or `UTC`. *Use
  this device's time zone* fills it in for you. Until you choose, the
  server's default applies. Lists say which zone their times are in, and
  date filters and deadlines follow it;
* **notifications** (exercise leaders and groups) – a desktop notification
  and/or a short sound when a new report or chat message arrives while the
  site is open in a browser tab. The browser asks for permission the first
  time. The notification never shows what the report or message says.

All of these are remembered in the browser you are using, not on the
account, so everyone sharing a group account can have their own. See
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
  group. Normally nobody else can read it, not even the exercise leader –
  unless *oversight* is switched on, see below.

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

### Oversight by the exercise leader

An exercise can be run with **oversight** of the chat between groups. You
can always tell:

* a notice above the conversation says *Oversight is on: the exercise
  leader can read the messages you send here*;
* every message the exercise leader can read carries the tag *visible to
  the exercise leader*.

Only messages sent while oversight is on can be read by the exercise
leader. Messages without the tag stay private, even if oversight is
switched on later.

Oversight can be switched on or off during the exercise. If you have a
conversation open, the notice appears or disappears by itself within a few
seconds. Should you press **Send** just after oversight was switched on,
before you could see the notice, your message is **not sent**: the page
shows the notice and keeps your text, and you choose whether to send it.

**For exercise leaders:** the chat page has a section *Oversight of group
conversations* below your own conversations. It shows whether oversight is
on, who last changed it, and a button **Switch oversight on** / **Switch
oversight off**. Switching it on tells the groups at once and lets you read
what they write to each other *from then on*; switching it off makes their
conversations private again and closes your view. All exercise leaders
share the switch. If the page says the setting is made in the server
configuration, the person running the server has reserved the decision.

With oversight on, the conversations between groups are listed there. Opening
one shows the messages the two groups have exchanged under oversight. You
can read but not write there – use the group's own conversation to contact
a group. New messages appear by themselves within a few seconds, as in any
conversation. Each time you open one, and each time new messages are shown
to you, that is recorded in the server's audit log.

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

### Drafts

You do not have to finish a report in one go. **Save as draft** keeps what
you have written without sending it, even if required fields are still
empty; while you write, the form is also saved automatically every few
seconds (when JavaScript is on). Your drafts are listed on **Home** under
*Your drafts*; opening the same template or request again continues where
you left off. Sending the report removes the draft, and you can delete one
from the list. A draft is yours alone: neither your group nor the exercise
leader can see it. You can keep up to 20.

### Answer a request

Requests from the exercise leader are listed on **Home** and under
**Requests**. Press **Answer**: the form uses the template the leader
attached and the answer always goes to the exercise leader. You can answer
a request more than once until the leader closes it.

A request can have a **deadline**, shown next to it. When it has passed and
your group has not answered, the request is marked *overdue*. You can still
answer; the answer is then marked *after the deadline*.

### Inbox and sent reports

* **Inbox** – reports other groups addressed to your group. It works like
  the leader's queue (see below), including filters, saved filters and
  export.
* **Sent** – everything your group has sent. *Export this list (CSV)* saves
  it as a file for a spreadsheet.

## For the exercise leader

### The queue

**Queue** shows the incoming reports. The panel on the left has **one queue
per group** plus *All queues*; the red badge counts the reports **you**
have not opened yet. Those rows are bold and marked with a dot.

* The list **updates by itself** about every 10 seconds: new reports
  appear, counters change, and the boxes you have ticked stay ticked.
* **Filter** by status, template, folder, request, assignee, date range,
  or *Only unread*.
* **Search** with one or more words; all words must occur in the subject,
  content, sender, template, folder or a field value.
* **Sort** by clicking a column heading; click again to reverse.
* On other pages, a notice appears at the top when new reports arrive and
  the counter in the menu updates – nothing you are typing is lost.
* **Saved filters:** set up a view you use often, type a name under *Save
  the current view as* and press **Save filter**. It then appears in the
  left panel for one-click access. Saved filters are your own.
* **Export this list (CSV)** downloads exactly the reports the current
  filter shows, one row per report with the template fields as columns,
  for a spreadsheet. Use **Print** on a report page for paper.
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

The status is shared by all exercise leaders: `new` means nobody has opened
the report yet. What is *unread* is personal – a report a colleague has
opened is still bold for you until you open it yourself.

**Assigning:** to divide the work, tick reports and choose *assign to me*,
or pick a leader under *Assigned to* on the report page. Filter on
*Assigned to: me* (or *nobody*) to see your share. Assignment is a label;
it does not stop a colleague from opening or filing the report.

Opening a report marks it as read. To **file** reports, tick them, choose
*file in folder*, type a folder name (existing names are suggested) and
press **Apply** – or use the **File** button on the report page. Filed
reports stay searchable; filter on *Status: filed* or on the folder to find
them again. *Mark as new* puts a report back at the top of your attention.

### Send a question or information request

**Requests → New request**: write the subject and the question, choose the
**report template** the groups shall answer with, optionally a **deadline**
(in your time zone), and choose the recipients (all groups or selected
ones). After the deadline, groups that have not answered are marked
*overdue* on the request page and late answers are marked as such. You can
move or remove the deadline on the request page. The request page shows which groups have
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

### Starting a new exercise

**Exercise** shows how much the current exercise holds and lets you start a
new one. Type the name of the exercise that is ending (optional), type
`RESET` in the confirmation field and press **Archive and start a new
exercise**. The whole database is first archived on the server; then all
reports, requests, chat messages and drafts are removed. Accounts,
templates and saved filters stay, so the next exercise can begin at once.
This cannot be undone from the user interface – export what you want to
keep at hand first (*Export all reports to the exercise leader (CSV)* on
the same page). Administrators can also start a new exercise, but cannot
export reports.

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

To create a **personal account for a member of a group**, choose the role
`group` and pick the group under *Member of group*. Leave that empty for
the group's own account. The group cannot be changed afterwards.

For each account you can **Rename** (display name), **Reset password**
(new one-time password, all sessions of the account end) and **Disable** /
**Enable**. Accounts are never deleted, so old reports keep a valid sender.
Disabling a group's account disables the whole group, including its
members' personal accounts.

A typical exercise set-up: one `leader` account per exercise leader, one
`group` account per group, and – if you want to know who wrote what – one
personal account per participant.

### Many accounts at once

**Accounts → Create several accounts at once**: paste or type one account
per line as `username; display name; role; group` (semicolons, commas or
tabs – a spreadsheet selection pastes directly). The role may be left out
(`group`); naming a group's username in the fourth position makes a
personal account in that group, and the group can be created on an earlier
line:

```
alpha; Group Alpha
anna; Anna Andersson; group; alpha
bo; Bo Berg; group; alpha
staff1; Maria Lind; leader
```

If any line is wrong, nothing is created and the page lists what to
correct. Otherwise all accounts are created and the next page shows every
username with its one-time password – **once**. Press **Print** and cut
the list into slips. Up to 200 accounts per go.

### Backup

**Backup → Make a backup now** writes a complete copy of the database to
the server, while everyone keeps working. The page lists the backups that
exist. They cannot be downloaded here, because they contain everything,
including what administrators cannot see in the interface; the person who
runs the server copies them elsewhere and may have set up automatic
backups.

### Audit log

**Audit log** lists who did what and when: logins and failed logins,
account changes, sent reports, queue actions, exports, oversight changes,
backups and resets, with the user and the address they came from. Search
for a username, an event or an address. It never contains passwords,
report text or chat messages.
