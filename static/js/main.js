var Timer = React.createClass({
    getInitialState: function() {
        return {
            secs: this.props.secs,
            timer: null
        };
    },
    onTick: function() {
        this.setState({
            secs: this.state.secs - 1
        });
    },
    componentDidMount: function() {
        var timer = setInterval(this.onTick, 1000);
        this.setState({ timer: timer });
    },
    componentWillUnmount: function() {
        if (this.state.timer) {
            clearInterval(this.state.timer);
        }
    },
    componentWillReceiveProps: function(nextProps) {
        clearInterval(this.state.timer);
        this.setState({
            secs: this.props.secs
        });
        var timer = setInterval(this.onTick, 1000);
        this.setState({ timer: timer });
    },
    render: function() {
        var current = this.state.secs
        var hours = Math.floor(this.state.secs / 3600);
        current = current % 3600;

        var minutes = Math.floor(current / 60);
        current = current % 60;

        var secs = current;
        return (
            <span>{hours}:{minutes}:{secs}</span>
        );
    }
});

var Tile = React.createClass({
    getInitialState: function() {
        return {
            currentMessage: "",
            message: "",
            timer: null
        };
    },
    onChange: function(event) {
        this.setState({currentMessage: event.target.value});
    },
    dataStates: {
        "LOCKED_BY_CURRENT_USER": "Locked by current user",
        "LOCKED_BY_OTHER": "Locked by other",
        "PURCHASED": "Purchased",
        "OPEN": "Open"
    },
    onArrowClicked: function() {
        this.setState({message: this.state.currentMessage});
        this.props.onArrowClicked(this.props.idx);
    },
    checkAndPurchaseTile: function() {
        var isCorrectTile = this.props.dataState == 'LOCKED_BY_CURRENT_USER';
        var balanceSuccessful = this.props.balance >= this.props.price;
        if (isCorrectTile && balanceSuccessful) {
            $.post("/purchase", JSON.stringify({
                "frame_number": this.props.idx,
                "message": this.state.message,
            }), function(res) {
                self.reloadAddresses();
            });
        }
    },
    componentWillReceiveProps: function(nextProps) {
        if (nextProps.dataState == 'LOCKED_BY_CURRENT_USER' && this.state.message.length > 0) {
            if (!this.state.timer) {
                var timer = setInterval(this.checkAndPurchaseTile, 2000);
                console.log("TIMER: " + timer);
                this.setState({timer: timer});
                console.log("Created timer for tile " + this.props.idx)
            }
        } else {
            if (this.state.timer) {
                clearInterval(this.state.timer);
                this.setState({timer: null});
                console.log("Deleted timer for tile " + this.props.idx)
            }
        }
    },
    renderOpen: function() {
        var nextBtnClasses = "next-btn glyphicon glyphicon-play";
        if (this.state.currentMessage.length === 0) {
            nextBtnClasses += ' hide-text';
        }
        return (
            <div className="tile">
                <span onClick={this.onArrowClicked} className={nextBtnClasses} aria-hidden="true"></span>
                <div className="header text-center">
                    AVAILABLE
                </div>
                <div className="body text-center">
                    <textarea className="text-center" value={this.state.currentMessage} type="text" onChange={this.onChange} />
                </div>
            </div>
        );
    },
    renderLockedByCurrentUser: function() {
        var nextBtnClasses = "next-btn glyphicon glyphicon-play";
        var qrCode = "https://chart.googleapis.com/chart?chs=95x95&cht=qr&chl=" + this.props.address;
        if (this.balance == 0) {
            nextBtnClasses += ' hide-text';
        }
        return (
            <div className="tile">
                <div className="header text-center">
                   LOCKED FOR <Timer secs={this.props.ttl} />
                </div>
                <div className="body text-center">
                   <h3>SCAN QR CODE</h3>
                   <img className="center-block" src={qrCode} />
                </div>
            </div>
        );
    },
    renderLockedByOther: function() {
        return (
            <div className="tile">
                <div className="header text-center">
                   LOCKED FOR <Timer secs={this.props.ttl} />
                </div>
                <div className="body text-center pending-tile">
                   <h3>Someone is buying this tile</h3>
                </div>
            </div>
        );
    },
    renderPurchased: function() {
        return (
            <div className="tile">
                <div className="header text-center">
                   EXPIRES IN <Timer secs={this.props.ttl} />
                </div>
                <div className="body text-center purchased-tile">
                   <h3>{this.props.purchasedMessage}</h3>
                </div>
            </div>
        );
    },
    render: function() {
        if (this.props.dataState == 'PURCHASED') {
            return this.renderPurchased();
        }

        if (this.props.dataState == 'LOCKED_BY_OTHER') {
            return this.renderLockedByOther();
        }

        if (this.props.dataState == 'LOCKED_BY_CURRENT_USER' && this.state.message.length > 0) {
            return this.renderLockedByCurrentUser();
        }

        return this.renderOpen();
    }
});

var MainComponent = React.createClass({
  getInitialState: function() {
      return { addresses: [], tiles: [], balance: null };
  },
  reloadAddresses: function() {
      var self = this;
      var addressesRequest = $.getJSON('/addresses');
      var tilesRequest = $.getJSON('/tiles');
      $.when(addressesRequest, tilesRequest).then(function(a, b) {
          self.setState({
              addresses: a[0],
              tiles: b[0],
              balance: a[0][0].balance,
          });
      });
  },
  componentDidMount: function() {
      var self = this;
      this.reloadAddresses();
      setInterval(function() {
          self.reloadAddresses();
      }, 3000);

      $.getJSON('/price').then(function(res) {
          self.setState({'price': res.price});
      });
  },
  lockTable: function(idx) {
      var self = this;
      $.post("/tile", JSON.stringify({
          "frame_number": idx
      }), function(res) {
          self.reloadAddresses();
      });
  },
  render: function() {
    var tiles = [];
    for (var i=0; i < this.state.addresses.length; i++) {
        var balance = this.state.balance;
        var address = this.state.addresses[i].address;
        var tileData = this.state.tiles[i];
        var tile = (
            <div key={i} className="col-md-2 col-sm-2">
                <Tile
                 price={this.state.price}
                 idx={i}
                 onArrowClicked={this.lockTable}
                 dataState={tileData.state}
                 ttl={tileData.ttl}
                 address={address}
                 purchasedMessage={tileData.message}
                 balance={balance} />
            </div>
        );
        tiles.push(tile);
    }

    return (
      <div>
         {tiles}
      </div>
    );
  }
});

ReactDOM.render(
  <MainComponent />,
  document.getElementsByClassName('main')[0]
);
