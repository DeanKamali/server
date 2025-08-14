#ifndef SQL_SQL_SYS_OR_DDL_TRIGGER_H
#define SQL_SQL_SYS_OR_DDL_TRIGGER_H

#include "sql_trigger.h"  // trg_all_events_set, TRG_EVENT_MAX

/**
  Type representing events for system triggers (on logon, on logoff,
  on startup, on shutdown) and ddl triggers
*/
enum trg_sys_event_type
{
  TRG_SYS_EVENT_MIN= TRG_EVENT_MAX,
  TRG_EVENT_STARTUP= TRG_SYS_EVENT_MIN,
  TRG_EVENT_SHUTDOWN,
  TRG_EVENT_LOGON,
  TRG_EVENT_LOGOFF,
  TRG_EVENT_DDL,
  TRG_SYS_EVENT_MAX
};

static inline trg_all_events_set sys_trg2bit(enum trg_sys_event_type trg)
{ return static_cast<trg_all_events_set>(1 << static_cast<int>(trg)); }

static inline bool is_sys_trg_events(trg_all_events_set events)
{
  static const trg_all_events_set sys_events= trg_all_events_set(0) |
                                              sys_trg2bit(TRG_EVENT_LOGON) |
                                              sys_trg2bit(TRG_EVENT_LOGOFF) |
                                              sys_trg2bit(TRG_EVENT_STARTUP) |
                                              sys_trg2bit(TRG_EVENT_SHUTDOWN);
  /*
    Return true in case any of system events is set in the mask
  */
  return ((events & sys_events) != 0);
}

static inline bool is_ddl_trg_events(trg_all_events_set events)
{
  static const trg_all_events_set ddl_events= trg_all_events_set(0) |
                                              sys_trg2bit(TRG_EVENT_DDL);
  /*
    Return true in case the only TRG_EVENT_DDL bit is set in the mask,
    that is the trigger is solely for handling DDL events
  */
  return ((events & ddl_events) == events);
}

class Sys_trigger
{
public:
  Sys_trigger(sp_head *sp)
  : m_sp{sp} {}

  bool execute(THD *thd);

private:
  sp_head *m_sp;
};
#endif /* SQL_SQL_SYS_OR_DDL_TRIGGER_H */
