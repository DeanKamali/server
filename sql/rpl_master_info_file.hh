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

#include "rpl_info_file.hh"
#include <unordered_map> // Type of @ref MasterInfoFile::FIELDS_MAP
#include <string_view>   // Key type of @ref MasterInfoFile::FIELDS_MAP
#include <unordered_set> // Parameter type of ChangeMaster::set_defaults() //?
#include <optional>      // Storage type of @ref OptionalIntField
//#include "sql_const.h"   // MAX_PASSWORD_LENGTH


/// enum for @ref MasterInfoFile::master_use_gtid
enum struct enum_master_use_gtid { NO, CURRENT_POS, SLAVE_POS, DEFAULT };
/// String names for non-@ref enum_master_use_gtid::DEFAULT values
inline const char *master_use_gtid_names[]=
  {"No", "Current_Pos", "Slave_Pos", nullptr};

/**
  `mariadbd` Options for the `DEFAULT` values of @ref MasterInfoFile fields
  @{
*/
inline uint32_t master_connect_retry= 60;
inline std::optional<uint32_t> master_heartbeat_period= std::nullopt;
inline bool master_ssl= true;
inline const char *master_ssl_ca     = "";
inline const char *master_ssl_capath = "";
inline const char *master_ssl_cert   = "";
inline const char *master_ssl_crl    = "";
inline const char *master_ssl_crlpath= "";
inline const char *master_ssl_key    = "";
inline const char *master_ssl_cipher = "";
inline bool master_ssl_verify_server_cert= true;
/// `ulong` is the data type `my_getopt` expects.
inline auto master_use_gtid= static_cast<ulong>(enum_master_use_gtid::DEFAULT);
inline uint64_t master_retry_count= 100'000;
/// }@


struct MasterInfoFile: InfoFile
{

  /** General Optional Field
    @tparam T wrapped type
 */
  template<typename T> struct OptionalField: virtual Persistent
  {
    std::optional<T> optional;
    virtual operator T()= 0;
    auto &operator=(T value)
    {
      optional.emplace(value);
      return *this;
    }
    bool is_default() override { return optional.has_value(); }
    bool set_default() override
    {
      optional.reset();
      return false;
    }
  };
  /** Integer Field with `DEFAULT`
    @tparam mariadbd_option
      server options variable that determines the value of `DEFAULT`
    @tparam I integer type (auto-deduced from `mariadbd_option`)
    @see IntField version without `DEFAULT` (not a superclass)
  */
  template<auto &mariadbd_option,
           typename I= std::remove_reference_t<decltype(mariadbd_option)>>
  struct OptionalIntField: OptionalField<I>
  {
    using OptionalField<I>::operator=;
    operator I() override
    { return OptionalField<I>::optional.value_or(mariadbd_option); }
    virtual bool load_from(IO_CACHE *file) override
    { return IntIOCache::from_chars<I>(file, this); }
    virtual void save_to(IO_CACHE *file) override
    { return IntIOCache::to_chars(file, operator I()); }
  };

  /** SSL Path Field:
    @ref FN_REFLEN-sized C-string with a `mariadbd` option for the `DEFAULT`.
    Empty string is "\0\0" and `DEFAULT`ed string is "\0\1".
  */
  template<const char *const &mariadbd_option>
  struct OptionalPathField: StringField<>
  {
    operator const char *() override
    {
      if (is_default())
        return mariadbd_option;
      return StringField<>::operator const char *();
    }
    auto &operator=(const char *other)
    {
      buf[1]= false; // not default
      StringField<>::operator=(other);
      return *this;
    }
    bool is_default() override { return !buf[0] && buf[1]; }
    bool set_default() override
    {
      buf[0]= false;
      buf[1]= true;
      return false;
    }
    bool load_from(IO_CACHE *file) override
    {
      buf[1]= false; // not default
      return StringField<>::load_from(file);
    }
  };

  /** Boolean Field with `DEFAULT`:
    This uses a trilean enum,
    which is more efficient than `std::optional<bool>`.
    load_from() and save_to() are also engineered
    to make use of the range of only two cases.
  */
  template<bool &mariadbd_option> struct OptionalBoolField: Persistent
  {
    enum { NO, YES, DEFAULT= -1 } value;
    operator bool() { return is_default() ? mariadbd_option : (value != NO); }
    bool is_default() override { return value <= DEFAULT; }
    bool set_default() override
    {
      value= DEFAULT;
      return false;
    }
    auto &operator=(bool value)
    {
      this->value= value ? YES : NO;
      return *this;
    }
    /// @return `true` if the line is `0` or `1`, `false` otherwise or on error
    bool load_from(IO_CACHE *file) override
    {
      /** Only three chars are required:
      * One digit
        (When base prefixes are not recognized in integer parsing,
        anything with a leading `0` stops parsing
        after converting the `0` to zero anyway.)
      * the terminating `\n\0` as in IntegerLike::from_chars(IO_CACHE *, I &)
      */
      char buf[3];
      if (my_b_gets(file, buf, 3))
        switch (buf[0])
        {
          case '0':
            value= NO;
            return false;
          case '1':
            value= YES;
            return false;
        }
      return true;
    }
    void save_to(IO_CACHE *file) override
    { my_b_write_byte(file, operator bool() ? '1' : '0'); }
  };

  /** ID Array field
    @deprecated
      Only one of `DO_DOMAIN_IDS` and `IGNORE_DOMAIN_IDS` can be active
      at a time, so giving them separate arrays, let alone field instances,
      is wasteful. Until we refactor this pair, this will only reference
      to existing arrays to reduce changes that will be obsolete by then.
      As references, the struct does not manage (construct/destruct) the array.
  */
  struct IDArrayField: Persistent
  {
    DYNAMIC_ARRAY &array;
    IDArrayField(DYNAMIC_ARRAY &array): array(array) {}
    operator DYNAMIC_ARRAY &() { return array; }
    bool load_from(IO_CACHE *file) override
    { return init_dynarray_intvar_from_file(&array, file); }
    /** Store the total number of elements followed by the individual elements.
      Unlike the old `Domain_id_filter::as_string()`,
      this implementation does not require allocating the heap temporarily.
    */
    void save_to(IO_CACHE *file) override
    {
      IntIOCache::to_chars(file, array.elements);
      for (size_t i= 0; i < array.elements; ++i)
      {
        /**
          matches the type of the array
          (FIXME: Domain and Server IDs should be `uint32_t`s.)
        */
        ulong id;
        get_dynamic(&array, &id, i);
        my_b_write_byte(file, ' ');
        IntIOCache::to_chars(file, id);
      }
    }
  };


  /**
    `@@master_info_file` fields, in SHOW SLAVE STATUS order where applicable
    @{
  */

  StringField<HOSTNAME_LENGTH*SYSTEM_CHARSET_MBMAXLEN + 1> master_host;
  StringField<USERNAME_LENGTH + 1> master_user;
  // Not in SHOW SLAVE STATUS
  StringField<MAX_PASSWORD_LENGTH*SYSTEM_CHARSET_MBMAXLEN + 1> master_password;
  IntField<uint32_t> master_port;
  /// Connect_Retry
  OptionalIntField<::master_connect_retry> master_connect_retry;
  StringField<> master_log_file;
  /// Read_Master_Log_Pos
  IntField<my_off_t> master_log_pos;
  /// Master_SSL_Allowed
  OptionalBoolField<::master_ssl> master_ssl;
  /// Master_SSL_CA_File
  OptionalPathField<::master_ssl_ca> master_ssl_ca;
  /// Master_SSL_CA_Path
  OptionalPathField<::master_ssl_capath> master_ssl_capath;
  OptionalPathField<::master_ssl_cert> master_ssl_cert;
  OptionalPathField<::master_ssl_cipher> master_ssl_cipher;
  OptionalPathField<::master_ssl_key> master_ssl_key;
  OptionalBoolField<::master_ssl_verify_server_cert>
    master_ssl_verify_server_cert;
  /// Replicate_Ignore_Server_Ids
  IDArrayField ignore_server_ids;
  OptionalPathField<::master_ssl_crl> master_ssl_crl;
  OptionalPathField<::master_ssl_crlpath> master_ssl_crlpath;

  /** @ref enum_master_use_gtid (with `DEFAULT`) Field:
    It has a `DEFAULT` value of @ref ::master_use_gtid,
    which in turn has a `DEFAULT` value based on @ref gtid_supported.
  */
  struct: Persistent
  {
    enum_master_use_gtid mode;
    /**
      The default `master_use_gtid` is normally `SLAVE_POS`; however, if the
      master does not supports GTIDs, we fall back to `NO`. This field caches
      the check so future RESET SLAVE commands don't revert to `SLAVE_POS`.
      load_from() and save_to() are engineered (that is, hard-coded)
      on the single-digit range of @ref enum_master_use_gtid,
      similar to OptionalBoolField.
    */
    bool gtid_supported= true;
    operator enum_master_use_gtid()
    {
      if (is_default())
      {
        auto default_use_gtid=
          static_cast<enum_master_use_gtid>(::master_use_gtid);
        return default_use_gtid >= enum_master_use_gtid::DEFAULT ? (
          gtid_supported ?
            enum_master_use_gtid::SLAVE_POS : enum_master_use_gtid::NO
        ) : default_use_gtid;
      }
      return mode;
    }
    auto &operator=(enum_master_use_gtid mode)
    {
      this->mode= mode;
      DBUG_ASSERT(!is_default());
      return *this;
    }
    bool is_default() override
    { return mode <= enum_master_use_gtid::DEFAULT; }
    bool set_default() override
    {
      mode= enum_master_use_gtid::DEFAULT;
      return false;
    }
    /** @return
      `true` if the line is a @ref enum_master_use_gtid,
      `false` otherwise or on error
    */
    bool load_from(IO_CACHE *file) override
    {
      /**
        Only two chars are required for the enum,
        similar to @ref OptionalBoolField::load_from()
      */
      char buf[2];
      if (!my_b_gets(file, buf, 2) ||
          buf[0] > /* SLAVE_POS */ '2' || buf[0] < /* NO */ '0')
        return true;
      operator=(static_cast<enum_master_use_gtid>(buf[0] - '0'));
      return false;
    }
    void save_to(IO_CACHE *file) override
    {
      my_b_write_byte(file,
        '0' + static_cast<unsigned char>(operator enum_master_use_gtid()));
    }
  }
  /// Using_Gtid
  master_use_gtid;

  /// Replicate_Do_Domain_Ids
  IDArrayField do_domain_ids;
  /// Replicate_Ignore_Domain_Ids
  IDArrayField ignore_domain_ids;
  OptionalIntField<::master_retry_count> master_retry_count;

  /**
    This is a non-negative `DECIMAL(10,3)` seconds field internally
    calculated as an unsigned integer milliseconds field.
    It has a `DEFAULT` value of @ref ::master_heartbeat_period,
    which in turn has a `DEFAULT` value of `@@slave_net_timeout / 2` seconds.
  */
  struct: OptionalField<uint32_t>
  {
    using OptionalField::operator=;
    operator uint32_t() override
    {
      return is_default() ?
        ::master_heartbeat_period.value_or(slave_net_timeout*500) :
        *(OptionalField<uint32_t>::optional);
    }
    bool load_from(IO_CACHE *file) override
    {
      /// Read in floating point first to validate the range
      double seconds;
      /**
        Number of chars OptionalIntField::load_from() uses plus
        1 for the decimal point; truncate the excess precision,
        which there should not be unless the file is edited externally.
      */
      char buf[IntIOCache::BUF_SIZE<uint32_t> + 3];
      size_t size= my_b_gets(file, buf, sizeof(buf));
      if (!size ||
          std::from_chars(buf, &buf[size], seconds,
                          std::chars_format::fixed).ec != IntIOCache::ERRC_OK ||
          seconds < 0 || seconds > SLAVE_MAX_HEARTBEAT_PERIOD) // 2**32 / 1000
        return true;
      operator=(seconds / 1000);
      return false;
    }
    /**
      This method is engineered (that is, hard-coded) to take
      full advantage of the non-negative `DECIMAL(10,3)` format.
    */
    void save_to(IO_CACHE *file) override {
      char buf[IntIOCache::BUF_SIZE<uint32_t>];
      std::to_chars_result result=
        std::to_chars(buf, &buf[sizeof(buf)], operator uint32_t());
      DBUG_ASSERT(result.ec == IntIOCache::ERRC_OK);
      ptrdiff_t size= result.ptr - buf;
      if (size > 3) // decimal seconds has ones digit or more
      {
        my_b_write(file, (const uchar *)buf, size - 3);
        my_b_write_byte(file, '.');
        my_b_write(file, &(const uchar &)result.ptr[-3], 3);
      }
      else
      {
        my_b_write_byte(file, '0');
        my_b_write_byte(file, '.');
        for (ptrdiff_t zeroes= size; zeroes < 3; ++zeroes)
          my_b_write_byte(file, '0');
        my_b_write(file, (const uchar *)buf, size);
      }
    }
  }
  /// `Slave_heartbeat_period` of SHOW ALL SLAVES STATUS
  master_heartbeat_period;

  /// }@


  inline static const std::initializer_list<mem_fn> FIELDS_LIST= {
    &MasterInfoFile::master_log_file,
    &MasterInfoFile::master_log_pos,
    &MasterInfoFile::master_host,
    &MasterInfoFile::master_user,
    &MasterInfoFile::master_password,
    &MasterInfoFile::master_port,
    &MasterInfoFile::master_connect_retry,
    &MasterInfoFile::master_ssl,
    &MasterInfoFile::master_ssl_ca,
    &MasterInfoFile::master_ssl_capath,
    &MasterInfoFile::master_ssl_cert,
    &MasterInfoFile::master_ssl_cipher,
    &MasterInfoFile::master_ssl_key,
    &MasterInfoFile::master_ssl_verify_server_cert,
    &MasterInfoFile::master_heartbeat_period,
    // &MasterInfoFile::master_bind, // MDEV-19248
    &MasterInfoFile::ignore_server_ids,
    nullptr, // MySQL field `master_uuid`, which MariaDB ignores.
    &MasterInfoFile::master_retry_count,
    &MasterInfoFile::master_ssl_crl,
    &MasterInfoFile::master_ssl_crlpath
  };

  /**
    Guard agaist extra left-overs at the end of file in case a later update
    causes the effective content to shrink compared to earlier contents
  */
  static constexpr const char END_MARKER[]= "END_MARKER";
  /// An iterable for the `key=value` section of `@@master_info_file`
  // C++ default allocator to match that `mysql_execute_command()` uses `new`
  inline static const std::unordered_map<std::string_view, mem_fn> FIELDS_MAP= {
    // These are here to annotate whether they are `DEFAULT`.
    {"connect_retry"         , &MasterInfoFile::master_connect_retry         },
    {"ssl"                   , &MasterInfoFile::master_ssl                   },
    {"ssl_ca"                , &MasterInfoFile::master_ssl_ca                },
    {"ssl_capath"            , &MasterInfoFile::master_ssl_capath            },
    {"ssl_cert"              , &MasterInfoFile::master_ssl_cert              },
    {"ssl_cipher"            , &MasterInfoFile::master_ssl_cipher            },
    {"ssl_key"               , &MasterInfoFile::master_ssl_key               },
    {"ssl_crl"               , &MasterInfoFile::master_ssl_crl               },
    {"ssl_crlpath"           , &MasterInfoFile::master_ssl_crlpath           },
    {"ssl_verify_server_cert", &MasterInfoFile::master_ssl_verify_server_cert},
    {"heartbeat_period"      , &MasterInfoFile::master_heartbeat_period      },
    {"retry_count"           , &MasterInfoFile::master_retry_count           },
    /* These are the ones new in MariaDB.
      For backward compatibility,
      keys should match the corresponding old property name in @ref Master_info.
    */
    {"using_gtid",        &MasterInfoFile::master_use_gtid  },
    {"do_domain_ids",     &MasterInfoFile::do_domain_ids    },
    {"ignore_domain_ids", &MasterInfoFile::ignore_domain_ids},
    {END_MARKER, nullptr}
  };


  MasterInfoFile(DYNAMIC_ARRAY &ignore_server_ids,
                 DYNAMIC_ARRAY &do_domain_ids, DYNAMIC_ARRAY &ignore_domain_ids)
    : ignore_server_ids(ignore_server_ids),
      do_domain_ids(do_domain_ids), ignore_domain_ids(ignore_domain_ids)
  {
    for(auto &[_, mem_fn]: FIELDS_MAP)
      if (static_cast<bool>(mem_fn))
        mem_fn(this).set_default();
  }
};
