/*
  Copyright (c) 2025 MariaDB

  This program is free software; you can redistribute it and/or modify
  it under the terms of the GNU General Public License as published by
  the Free Software Foundation; version 2 of the License.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.
  See the GNU General Public License for more details.

  You should have received a copy of the GNU General Public License along
  with this program; if not, write to the Free Software Foundation, Inc.,
  51 Franklin St, Fifth Floor, Boston, MA 02110-1335 USA.
*/

#include <optional>    // Storage type of @ref OptionalIntConfig
#include <my_global.h> // FN_REFLEN
#include <my_sys.h>    // IO_CACHE

/// Enum for @ref ChangeMaster::master_use_gtid
enum struct enum_master_use_gtid { NO, CURRENT_POS, SLAVE_POS, DEFAULT= -1 };
inline const char *const NAME_MASTER_USE_GTID[]=
  {"No", "Current_Pos", "Slave_Pos", nullptr};

// `mariadbd` Options for @ref ChangeMaster::WithDefault
inline uint32_t master_connect_retry= 60;
inline float master_heartbeat_period= -1.0;
inline bool master_ssl= true;
inline const char *master_ssl_ca     = "";
inline const char *master_ssl_capath = "";
inline const char *master_ssl_cert   = "";
inline const char *master_ssl_crl    = "";
inline const char *master_ssl_crlpath= "";
inline const char *master_ssl_key    = "";
inline const char *master_ssl_cipher = "";
inline bool master_ssl_verify_server_cert= true;
inline auto master_use_gtid= enum_master_use_gtid::DEFAULT;
inline uint64_t master_retry_count= 100'000;

/// Persistence interface for an unspecified item
struct Persistent
{
  inline virtual bool is_default() { return false; }
  /// @return `true` if the item is mandatory and couldn't provide a default
  inline virtual bool set_default() { return true; }
  /** set the value by reading a line from the IO and consume the `\n`
    @return `false` if successful or `true` if error
    @post is_default() is `false`
  */
  virtual bool load_from(IO_CACHE *file)= 0;
  /** write the *effective* value to the IO **without** a `\n`
    (The caller will separately determine how
     to represent using the default value.)
  */
  virtual void save_to(IO_CACHE *file)= 0;
  inline Persistent() { set_default(); }
  virtual ~Persistent()= default;
};

/** Struct for CHANGE MASTER configurations:
  Each config is an instance of an implementation of the
  @ref Persistent interface. In turn, this class's own Persistent method
  overrides iterates over them through the local listings in `change_master.cc`.
*/
struct ChangeMaster: Persistent
{
  /** Simple Integer config with `DEFAULT`
    @see master_connect_retry
    @see master_retry_count
  */
  template<auto &mariadbd_option> struct OptionalIntConfig: Persistent
  {
    using IntType= std::remove_reference_t<decltype(mariadbd_option)>;
    std::optional<IntType> optional;
    inline operator IntType()
      { return optional.value_or(mariadbd_option); }
    inline OptionalIntConfig &operator=(IntType value)
    {
      optional.emplace(value);
      return *this;
    }
    inline bool is_default() override { return optional.has_value(); }
    inline bool set_default() override
    {
      optional.reset();
      return false;
    }
    bool load_from(IO_CACHE *file) override;
    void save_to(IO_CACHE *file) override;
  };

  /// Singleton class for @ref master_heartbeat_period
  struct master_heartbeat_period_t: Persistent
  {
    float period;
    operator float();
    inline master_heartbeat_period_t &operator=(float period)
    {
      DBUG_ASSERT(period >= 0);
      this->period= period;
      return *this;
    }
    inline bool is_default() override { return period < 0; }
    inline bool set_default() override
    {
      period= -1.0;
      return false;
    }
    bool load_from(IO_CACHE *file) override;
    void save_to(IO_CACHE *file) override;
  };

  /** Simple boolean config with `DEFAULT`
    @see master_ssl
    @see master_ssl_verify_server_cert
  */
  template<bool &mariadbd_option> struct OptionalBoolConfig: Persistent
  {
    /// Trilean: Enum alternative for "optional<bool>"
    enum tril { NO, YES, DEFAULT= -1 } value;
    operator bool();
    inline bool is_default() override { return value <= tril::DEFAULT; }
    inline bool set_default() override
    {
      value= tril::DEFAULT;
      return false;
    }
    inline OptionalBoolConfig &operator=(bool value)
    {
      this->value= static_cast<tril>(value);
      return *this;
    }
    bool load_from(IO_CACHE *file) override;
    void save_to(IO_CACHE *file) override;
  };

  /** for SSL paths:
    They are @ref FN_REFLEN-sized null-terminated
    string buffers with `mariadbd` options as defaults.
  */
  template<const char *&mariadbd_option>
  struct OptionalPathConfig: Persistent
  {
    char path[FN_REFLEN];
    inline operator const char *() { return path; }
    /// Does nothing if `path` is `nullptr`
    OptionalPathConfig &operator=(const char *path);
    bool is_default() override;
    bool set_default() override;
    bool load_from(IO_CACHE *file) override;
    void save_to(IO_CACHE *file) override;
  };

  /// Singleton class for @ref master_use_gtid
  struct master_use_gtid_t: Persistent
  {
    enum_master_use_gtid mode;
    /**
      The default `master_use_gtid` is normally `SLAVE_POS`; however, if the
      master does not supports GTIDs, we fall back to `NO`. This field caches
      the check so future RESET SLAVE commands don't revert to `SLAVE_POS`.
    */
    bool gtid_supported= true;
    operator enum_master_use_gtid();
    inline master_use_gtid_t &operator=(enum_master_use_gtid mode)
    {
      this->mode= mode;
      DBUG_ASSERT(!is_default());
      return *this;
    }
    inline bool is_default() override
      { return mode <= enum_master_use_gtid::DEFAULT; }
    inline bool set_default() override
    {
      mode= enum_master_use_gtid::DEFAULT;
      return false;
    }
    /// @return `false` if the read integer is not a @ref enum_master_use_gtid
    bool load_from(IO_CACHE *file) override;
    void save_to(IO_CACHE *file) override;
  };

  /** for Domain ID arrays:
    They are currently **pointers to**'s @ref DYNAMIC_ARRAY's in the
    `Domain_id_filter`. Therefore, unlike `std::list<int32_t>`s,
    they do not manage (construct/destruct) these arrays and have no `DEFAULT`.
  */
  struct IDListConfig: Persistent
  {
    DYNAMIC_ARRAY *list;
    inline IDListConfig(DYNAMIC_ARRAY *list): list(list) {}
    inline operator DYNAMIC_ARRAY *() { return list; }
    bool load_from(IO_CACHE *file) override;
    /// Store the total number of elements followed by the individual elements.
    void save_to(IO_CACHE *file) override;
  };

  // CHANGE MASTER entries; here in SHOW SLAVE STATUS order
  OptionalIntConfig<::master_connect_retry> master_connect_retry;
  master_heartbeat_period_t master_heartbeat_period;
  OptionalBoolConfig<::master_ssl> master_ssl;
  OptionalPathConfig<::master_ssl_ca> master_ssl_ca;
  OptionalPathConfig<::master_ssl_capath> master_ssl_capath;
  OptionalPathConfig<::master_ssl_cert> master_ssl_cert;
  OptionalPathConfig<::master_ssl_crl> master_ssl_crl;
  OptionalPathConfig<::master_ssl_crlpath> master_ssl_crlpath;
  OptionalPathConfig<::master_ssl_key> master_ssl_key;
  OptionalPathConfig<::master_ssl_cipher> master_ssl_cipher;
  OptionalBoolConfig<::master_ssl_verify_server_cert>
    master_ssl_verify_server_cert;
  master_use_gtid_t master_use_gtid;
  IDListConfig do_domain_ids;
  IDListConfig ignore_domain_ids;
  OptionalIntConfig<::master_retry_count> master_retry_count;

  inline ChangeMaster(DYNAMIC_ARRAY m_domain_ids[2]):
    do_domain_ids(&m_domain_ids[0]), ignore_domain_ids(&m_domain_ids[1]) {}
  /**
    Load all configs (currently, only those in the `key-value` section that
    support the `DEFAULT` keyword) from the file, stopping at the `END_MARKER`
  */
  bool load_from(IO_CACHE *file) override;
  /**
    Save all configs (currently, only those in the `key-value` section that
    support the `DEFAULT` keyword), to the file, including the `END_MARKER`
  */
  void save_to(IO_CACHE *file) override;
};
